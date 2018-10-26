package artifactor

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"cloud.google.com/go/storage"
)

// number of seconds to set the cache-control:max-age=%v header too
const CacheControlMaxAge = 60

type Project struct {
	name      string
	gcsPrefix string
	urlPrefix string
}

func NewProject(opts *Options) Project {
	return Project{
		name:      opts.ProjectName,
		gcsPrefix: opts.GcsPrefix + opts.ProjectName + "/",
		urlPrefix: opts.UrlPrefix + opts.ProjectName + "/",
	}
}

type Options struct {
	Latest bool

	ProjectName, GcsPrefix, Version, Dir, UrlPrefix string
	Aliases                                         []string
}

type ComponentManifest struct {
	Timestamp     time.Time   `json:"timestamp"`
	UnixTimestamp int         `json:"unix_timestamp"`
	Project       string      `json:"project"`
	Version       string      `json:"version"`
	GCSPrefix     string      `json:"gcs_prefix"`
	Components    []Component `json:"components"`

	manifestFilepath  string
	signatureFilepath string
}

// NewComponentManifest: create a component manifest which specifies all of the
// components in the version. Errors out if the manifest exists already, or if
// the srcDir is not a directory
func NewComponentManifest(srcDir string, project string, version string, ts time.Time, components []Component) ComponentManifest {
	manifestFilepath := path.Join(srcDir, "manifest.json")
	signatureFilepath := manifestFilepath + ".asc.sig"
	return ComponentManifest{
		Timestamp:     ts,
		UnixTimestamp: int(ts.Unix()),
		Project:       project,
		Version:       version,
		Components:    components,

		manifestFilepath:  manifestFilepath,
		signatureFilepath: signatureFilepath,
	}
}

func (c ComponentManifest) write() error {
	jsonBytes, err := json.Marshal(c)
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(c.manifestFilepath, jsonBytes, 0644); err != nil {
		return err
	}

	return createSigFile(c.manifestFilepath, c.signatureFilepath)
}

type ChecksumManifest struct {
	components        []Component
	manifestFilepath  string
	signatureFilepath string
}

func NewChecksumManifest(components []Component) ChecksumManifest {
	manifestFilepath := "checksums"
	signatureFilepath := manifestFilepath + ".asc.sig"

	return ChecksumManifest{
		components:        components,
		manifestFilepath:  manifestFilepath,
		signatureFilepath: signatureFilepath,
	}
}

func (c ChecksumManifest) write() error {
	writer, err := os.Create(c.manifestFilepath)
	if err != nil {
		return err
	}

	longestFilepath := 0
	for _, c := range c.components {
		if len(c.Filepath) > longestFilepath {
			longestFilepath = len(c.Filepath)
		}
	}

	tabWriter := tabwriter.NewWriter(writer, 1, 8, 0, '\t', 0)

	for idx, component := range c.components {
		fmt.Fprintln(tabWriter, fmt.Sprintf("%s\t%s\t%s", component.Filepath, "md5   ", component.Md5Checksum))
		fmt.Fprintln(tabWriter, fmt.Sprintf("%s\t%s\t%s", component.Filepath, "sha256", component.Sha256Checksum))
		fmt.Fprintln(tabWriter, fmt.Sprintf("%s\t%s\t%s", component.Filepath, "sha384", component.Sha384Checksum))
		fmt.Fprintln(tabWriter, fmt.Sprintf("%s\t%s\t%s", component.Filepath, "sha512", component.Sha512Checksum))

		if idx < len(c.components)-1 {
			fmt.Fprintln(tabWriter, "")
		}
	}

	tabWriter.Flush()
	writer.Close()
	return createSigFile(c.manifestFilepath, c.signatureFilepath)
}

type Component struct {
	Filepath    string `json:"filepath"`
	GCSFilepath string `json:"gcs_filepath"`
	URL         string `json:"url"`
	Bytes       int64  `json:"bytes"`

	Md5Checksum    string `json:"md5_checksum"`
	Sha256Checksum string `json:"sha256_checksum"`
	Sha384Checksum string `json:"sha384_checksum"`
	Sha512Checksum string `json:"sha512_checksum"`
}

// NewComponent: initialize a component and it's checksums
func NewComponent(filepath string, gcsPrefix string, urlPrefix string) (Component, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return Component{}, err
	}

	byts, err := ioutil.ReadAll(file)
	if err != nil {
		return Component{}, err
	}
	file.Close()

	reader := bytes.NewReader(byts)

	hashes := []hash.Hash{
		md5.New(),
		sha256.New(),
		sha512.New384(),
		sha512.New512_256(),
	}
	checksums := make([]string, 4)

	for idx, h := range hashes {
		reader.Seek(0, 0)

		if _, err := io.Copy(h, reader); err != nil {
			return Component{}, err
		}

		checksums[idx] = fmt.Sprintf("%x", h.Sum(nil))
	}

	return Component{
		Filepath:    filepath,
		GCSFilepath: gcsPrefix + filepath,
		URL:         urlPrefix + filepath,
		Bytes:       reader.Size(),

		Md5Checksum:    checksums[0],
		Sha256Checksum: checksums[1],
		Sha384Checksum: checksums[2],
		Sha512Checksum: checksums[3],
	}, nil
}

// uploadAliasComponents: alias the given components into a new directory. Usually, this
// is used to alias the manifest.json and manifest.json.asc.sig files into the
// /latest subdir
func uploadAliasComponents(aliasPrefix string, components []Component) error {
	// rewrite the gcs filepath for each, while maintaining references to
	// all of the old filepaths!
	for idx, component := range components {
		components[idx].GCSFilepath = aliasPrefix + component.Filepath
	}

	return uploadComponents(aliasPrefix, components)
}

// createComponents: create a set of components given an input directory. Return
// an error if no components found
func createComponents(srcDir, gcsPrefix string, urlPrefix string) ([]Component, error) {
	components := make([]Component, 0, 0)

	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// built in files that are managed by the artifactor do not get injected into the artifact manifest
		for _, bannedFilepath := range []string{"manifest.json", "manifest.json.asc.sig", "checksums", "checksums.asc.sig"} {
			if path == bannedFilepath {
				return nil
			}
		}

		component, err := NewComponent(path, gcsPrefix, urlPrefix)
		if err != nil {
			return err
		}

		components = append(components, component)
		return nil
	}

	if err := filepath.Walk(srcDir, walkFn); err != nil {
		return []Component(nil), err
	}

	return components, nil
}

// createSigFile: create a signature file using the local gpg environment. This
// does not use the crypto packages, so that it can use gpg-agent which is
// often tunneled over ssh
func createSigFile(input, output string) error {
	cmd := exec.Command("gpg", "--yes", "--armor", "--output", output, "--detach-sig", input)
	return cmd.Run()
}

// CreateVersion: create and upload a project version given a component set
func CreateVersion(project Project, opts *Options) error {
	ts := time.Now()
	versionGCSPrefix := project.gcsPrefix + opts.Version + "/"
	versionURLPrefix := project.urlPrefix + opts.Version + "/"

	components, err := createComponents(".", versionGCSPrefix, versionURLPrefix)
	if err != nil {
		return err
	}

	componentManifest := NewComponentManifest(".", project.name, opts.Version, ts, components)
	if err := componentManifest.write(); err != nil {
		return err
	}

	checksumManifest := NewChecksumManifest(components)
	if err := checksumManifest.write(); err != nil {
		return err
	}

	newComponentFilepaths := []string{
		checksumManifest.manifestFilepath,
		checksumManifest.signatureFilepath,
		componentManifest.manifestFilepath,
		componentManifest.signatureFilepath,
	}
	newComponents := make([]Component, 0, len(newComponentFilepaths))
	for _, filepath := range newComponentFilepaths {
		component, err := NewComponent(filepath, versionGCSPrefix, versionURLPrefix)
		if err != nil {
			return err
		}

		components = append(components, component)
		newComponents = append(newComponents, component)
	}

	if err := uploadComponents(project.gcsPrefix, components); err != nil {
		return err
	}

	for _, alias := range opts.Aliases {
		aliasPrefix := project.gcsPrefix + alias + "/"
		if err := uploadAliasComponents(aliasPrefix, newComponents); err != nil {
			return err
		}
	}

	return nil
}

// uploadComponents: upload all components to their corresponding location in
// the storage bucket
func uploadComponents(gcsPrefix string, components []Component) error {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}

	fullPrefix := strings.TrimLeft(gcsPrefix, "gcs://")
	bucketName := strings.Split(fullPrefix, "/")[0]

	bucket := client.Bucket(bucketName)

	var wg sync.WaitGroup
	errCh := make(chan error, len(components))

	for _, component := range components {
		wg.Add(1)

		go func(component Component) {
			err := func() error {
				byts, err := ioutil.ReadFile(component.Filepath)
				if err != nil {
					return err
				}

				objectName := strings.TrimPrefix(component.GCSFilepath, "gcs://"+bucketName+"/")
				bucketObject := bucket.Object(objectName)
				writer := bucketObject.NewWriter(ctx)

				writer.SendCRC32C = true
				writer.CRC32C = crc32.Checksum(byts, crc32.MakeTable(crc32.Castagnoli))
				writer.ObjectAttrs.CacheControl = fmt.Sprintf("max-age=%v", CacheControlMaxAge)

				if _, err := writer.Write(byts); err != nil {
					return err
				}

				if err := writer.Close(); err != nil {
					return err
				}

				// set attributes on the object
				if err := bucketObject.ACL().Set(ctx, storage.AllUsers, storage.RoleReader); err != nil {
					return err
				}

				return nil
			}()

			if err != nil {
				errCh <- err
			}
			wg.Done()
		}(component)
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
	}

	return nil
}
