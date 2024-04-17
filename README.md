# apkrane

## Install

```
go install github.com/jonjohnsonjr/apkrane@latest
```

## Usage

### `apkrane ls`

List all the `*.apk` files in an `APKINDEX.tar.gz` with `ls`:

```
apkrane ls https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz | head
7zip-22.01-r0.apk
7zip-2201-r0.apk
7zip-2301-r0.apk
7zip-2301-r1.apk
7zip-doc-22.01-r0.apk
7zip-doc-2201-r0.apk
7zip-doc-2301-r0.apk
7zip-doc-2301-r1.apk
R-4.3.1-r0.apk
R-4.3.1-r1.apk
```

Download them using the `--full` flag to produce full URLs:

```
apkrane ls --full https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz | head | xargs -n1 -I{} wget {}
```

Use the `--json` flag to make it amenable to `jq` shenanigans, e.g. to see which package has the most versions:

```
apkrane ls --json https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz | jq .Name -r | sort | uniq -c | sort -nr | head
 246 vim
 222 renovate
 191 py3-botocore
 154 aws-cli
 130 terragrunt
 118 py3-sqlglot
  91 wolfictl
  89 rqlite-oci-entrypoint
  89 rqlite
  79 pulumi-language-python
```
