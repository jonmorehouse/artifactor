package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jonmorehouse/artifactor"
)

type errInvalidOption struct {
	msg string
}

func (e errInvalidOption) Error() string {
	return e.msg
}

func parseFlags() (artifactor.Options, error) {
	var latest bool
	flag.BoolVar(&latest, "latest", true, "-latest whether to create a latest alias")

	var projectName, gcsPrefix, urlPrefix, version, dir string
	flag.StringVar(&projectName, "project", "", "-project top level project name")
	flag.StringVar(&version, "version", "", "-version version name")
	flag.StringVar(&dir, "dir", "", "-dir input dir")
	flag.StringVar(&gcsPrefix, "gcs-prefix", "", "-gcs-prefix storage bucket address")
	flag.StringVar(&urlPrefix, "url-prefix", "", "-url-prefix for the public url used in the manifest")

	flag.Parse()

	if dir == "" {
		return artifactor.Options{}, errInvalidOption{"-dir is required"}
	}
	if version == "" {
		return artifactor.Options{}, errInvalidOption{"-version is required"}
	}

	if projectName == "" {
		return artifactor.Options{}, errInvalidOption{"-option is required"}
	}

	if gcsPrefix == "" || !strings.HasPrefix(gcsPrefix, "gcs://") {
		return artifactor.Options{}, errInvalidOption{"-gcs-prefix is required and must start with gcs://"}
	}

	if urlPrefix == "" || !strings.HasPrefix(urlPrefix, "https://") {
		return artifactor.Options{}, errInvalidOption{"-url-prefix is required and must start with https://"}
	}

	if !strings.HasSuffix(gcsPrefix, "/") {
		gcsPrefix = gcsPrefix + "/"
	}

	if !strings.HasSuffix(urlPrefix, "/") {
		urlPrefix = urlPrefix + "/"
	}

	aliases := make([]string, 0)
	if latest {
		aliases = append(aliases, "latest")
	}

	return artifactor.Options{
		Latest:      latest,
		ProjectName: projectName,
		GcsPrefix:   gcsPrefix,
		UrlPrefix:   urlPrefix,
		Aliases:     aliases,
	}, nil
}

func main() {
	opts, err := parseFlags()
	if err != nil {
		log.Fatal(err)
	}
	os.Chdir(opts.Dir)

	log.Println(fmt.Sprintf("creating version %s %s", opts.ProjectName, opts.Version))

	project := artifactor.NewProject(&opts)
	if err := artifactor.CreateVersion(project, &opts); err != nil {
		log.Fatal(err)
	}
}
