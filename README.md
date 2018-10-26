# Artifactor

Artifactor is a small tool for creating and upload signed and versioned sets of static files. Artifacts are great for managing binary releases.

## Artifact Version

An artifact usually corresponds to a set of binaries for a project at a specific git ref. For example, given a project `foo`, which, when built contains the following binaries:

- `foo_darwin_386`
- `foo_linux_386`

An artifact version would correspond to a series of files uploaded to `artifacts.jm.house` (or whichever url/gcs bucket is specified). The uploaded assets would include all of the aforementioned files, _as well as_ the following:

- `manifest.json` - a manifest which includes version meta information, as well as a list of all files with metadata
- `manifest.json.asc.sig` - a gpg "detached" signature of the `manifest.json` file
- `checksums` - a plaintext list of filenames and checksums
- `checksums.asc.sig` - a gpg "detached" signature of the `checksums` file


Given the artifacts server at `https://artifacts.jm.house` fetching a version `123` of project `foo` would consist of the following steps:


**1.** download both `https://artifacts.jm.house/foo/123/manifest.json` and `https://artifacts.jm.house/foo/123/manifest.json.asc.sig`

**2.** verify the signature contained in `manifest.json.asc.sig` with the `manifest.json` file using the _known_ public key that was used to create the signature. For example, the key found at `https://keybase.io/jonmorehouse/key.asc`.

```bash
$ curl https://keybase.io/jonmorehouse/key.asc | gpg --import
$ gpg --verify manifest.json.asc.sig manifest.json
```

**3.** Once the `manifest.json` file has been verified, it's metainformation can be used to download files contained in the artifact version.

Given the following component contained in the manifest, the `url` and any `_checksum` field could be used to download the file and verify it's contents.

```json
{
  "components": [
    {
      "filepath": "artifactor_darwin_386",
      "gcs_filepath": "gcs://jonmorehouse-public-artifacts/artifactor/bed4b3b/artifactor_darwin_386",
      "url": "https://artifacts.jm.house/artifactor/bed4b3b/artifactor_darwin_386",
      "bytes": 12801804,
      "md5_checksum": "604117cb2c5cba5689811e711800de29",
      "sha256_checksum": "08c66345777255d464a40b34f0bbd094f7d41cef5729964b80161aa9a290dde3",
      "sha384_checksum": "6eb922311190592aa215d734adaddb891b0605c2bef22ac4875e7601f659e56db2bc62573203212ada429a16ea629a5f",
      "sha512_checksum": "ac38af950f8655c4292624a572ee050646fb939877ab72b5368c2fd2ee30dcab"
    }
}
```


## Creating an artifact

**1.** Create a directory with any and all assets that are part of the artifact version. For instance:

```bash
/tmp/artifactor/bed4b3b
├── artifactor_darwin_386
├── artifactor_darwin_amd64
├── artifactor_linux_386
├── artifactor_linux_amd64
|── artifactor_linux_arm
```

**2.** Export google credentials file path using:

```bash
export GOOGLE_APPLICATION_CREDENTIALS=/tmp/gcp.json
```

**3.** Call artifactor to create an artifact version

```bash
$ artifactor -dir $dir \
  -version $(git rev-parse --short HEAD) \
  -latest \
  -project foobar \
  -gcs-prefix gcs://jonmorehouse-public-artifacts \
  -url-prefix https://artifacts.jm.house
```
