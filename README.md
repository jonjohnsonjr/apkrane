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

Count only the latest versions of every package:

```
apkrane ls --latest https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz | wc -l
    5791
```

Show all versions of vim:

```
apkrane ls -P vim https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz | tail
vim-9.1.0296-r0.apk
vim-9.1.0304-r0.apk
vim-9.1.0309-r0.apk
vim-9.1.0318-r0.apk
vim-9.1.0330-r0.apk
vim-9.1.0336-r0.apk
vim-9.1.0354-r0.apk
vim-9.1.0356-r0.apk
vim-9.1.0358-r0.apk
vim-9.1.0359-r0.apk
```

Show only the most recent version of `vim`:

```
apkrane ls --latest -P vim https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz
vim-9.1.0359-r0.apk
```

### `apkrane cp`

Copy APKs file from a remote repository.

```
apkrane cp go-1.22
2024/05/24 09:53:19 downloading 1 packages for x86_64
2024/05/24 09:53:19 downloading https://packages.wolfi.dev/os/x86_64/go-1.22-1.22.3-r0.apk
2024/05/24 09:53:43 wrote packages/x86_64/go-1.22-1.22.3-r0.apk
2024/05/24 09:53:44 downloading 1 packages for aarch64
2024/05/24 09:53:44 downloading https://packages.wolfi.dev/os/aarch64/go-1.22-1.22.3-r0.apk
2024/05/24 09:54:09 wrote packages/aarch64/go-1.22-1.22.3-r0.apk
```

By default it will download the latest version of the package, which you can disable with `--latest=false`.

By default it will download to `./packages`, which you can change with `--out-dir`/`-o`.

By default it will download for all architectures, which you can change with `--arch`/`-a`, which accepts comma-separated values

You can specify a different APK repository location with `--repo`/`-r`, which accepts values `wolfi` (default), `extras` and `enterprise`.
