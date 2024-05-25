package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/jonjohnsonjr/apkrane/internal/version"
	"github.com/spf13/cobra"
	"gitlab.alpinelinux.org/alpine/go/repository"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"
)

func main() {
	if err := cli().ExecuteContext(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func cli() *cobra.Command {
	cmd := &cobra.Command{
		Use: "apkrane",
	}

	cmd.AddCommand(ls())
	cmd.AddCommand(cp())

	return cmd
}

func fetchIndex(ctx context.Context, u string) (io.ReadCloser, error) {
	if u == "-" {
		return os.Stdin, nil
	}

	scheme, _, ok := strings.Cut(u, "://")
	if !ok || !strings.HasPrefix(scheme, "http") {
		return os.Open(u)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %q: %w", u, err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GET %q: status %d", u, resp.StatusCode)
	}

	return resp.Body, nil
}

func ls() *cobra.Command {
	var full bool
	var latest bool
	var j bool
	var packageFilter string

	cmd := &cobra.Command{
		Use:     "ls",
		Example: `apkrane ls https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			u := args[0]

			dir := strings.TrimSuffix(u, "/APKINDEX.tar.gz")

			in, err := fetchIndex(ctx, u)
			if err != nil {
				return err
			}
			defer in.Close()

			index, err := repository.IndexFromArchive(io.NopCloser(in))
			if err != nil {
				return fmt.Errorf("parsing %q: %w", u, err)
			}

			w := cmd.OutOrStdout()
			enc := json.NewEncoder(w)

			packages := index.Packages

			// TODO: origin filter as well?
			if packageFilter != "" {
				packages = slices.DeleteFunc(packages, func(pkg *repository.Package) bool {
					return pkg.Name != packageFilter
				})
			}

			if latest {
				packages = onlyLatest(packages)
			}

			for _, pkg := range packages {
				p := fmt.Sprintf("%s-%s.apk", pkg.Name, pkg.Version)
				u := fmt.Sprintf("%s/%s", dir, p)
				if j {
					if err := enc.Encode(pkg); err != nil {
						return fmt.Errorf("encoding %s: %w", pkg.Name, err)
					}
				} else if full {
					fmt.Fprintf(w, "%s\n", u)
				} else {
					fmt.Fprintf(w, "%s\n", p)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&packageFilter, "package", "P", "", "print only packages with the given name")
	cmd.Flags().BoolVar(&latest, "latest", false, "print only the latest version of each package")
	cmd.Flags().BoolVar(&full, "full", false, "print the full url or path")
	cmd.Flags().BoolVar(&j, "json", false, "print each package as json")

	return cmd
}

func onlyLatest(packages []*repository.Package) []*repository.Package {
	// by package
	highest := map[string]*repository.Package{}

	for _, pkg := range packages {
		got, err := version.Parse(pkg.Version)
		if err != nil {
			// TODO: We should really fail here.
			log.Printf("parsing %q: %v", pkg.Filename(), err)
			continue
		}

		have, ok := highest[pkg.Name]
		if !ok {
			highest[pkg.Name] = pkg
			continue
		}

		// TODO: We re-parse this for no reason.
		parsed, err := version.Parse(have.Version)
		if err != nil {
			// TODO: We should really fail here.
			log.Printf("parsing %q: %v", have.Version, err)
			continue
		}

		if version.Compare(*got, *parsed) > 0 {
			highest[pkg.Name] = pkg
		}
	}

	return maps.Values(highest)
}

func cp() *cobra.Command {
	var latest bool
	var indexURL, outDir, gcsPath string
	cmd := &cobra.Command{
		Use:     "cp",
		Aliases: []string{"copy"},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			errg, ctx := errgroup.WithContext(cmd.Context())

			repoURL := strings.TrimSuffix(indexURL, "/APKINDEX.tar.gz")
			arch := repoURL[strings.LastIndex(repoURL, "/")+1:]

			in, err := fetchIndex(ctx, indexURL)
			if err != nil {
				return fmt.Errorf("fetching %q: %w", indexURL, err)
			}
			defer in.Close()
			index, err := repository.IndexFromArchive(io.NopCloser(in))
			if err != nil {
				return fmt.Errorf("parsing %q: %w", indexURL, err)
			}

			wantSet := map[string]struct{}{}
			for _, p := range args {
				wantSet[p] = struct{}{}
			}
			var packages []*repository.Package
			for _, pkg := range index.Packages {
				if _, ok := wantSet[pkg.Name]; !ok {
					continue
				}
				packages = append(packages, pkg)
			}

			if latest {
				packages = onlyLatest(packages)
			}

			if len(packages) == 0 {
				return fmt.Errorf("no packages found")
			}

			log.Printf("downloading %d packages for %s", len(packages), arch)

			for _, pkg := range packages {
				pkg := pkg
				errg.Go(func() error {
					fn := filepath.Join(outDir, arch, pkg.Filename())
					if _, err := os.Stat(fn); err == nil {
						log.Printf("skipping %s: already exists", fn)
						return nil
					}

					var rc io.ReadCloser
					if gcsPath == "" {
						url := fmt.Sprintf("%s/%s", repoURL, pkg.Filename())
						log.Println("downloading", url)
						req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
						if err != nil {
							return err
						}
						resp, err := http.DefaultClient.Do(req)
						if err != nil {
							return err
						}

						if err := os.MkdirAll(filepath.Join(outDir, arch), 0755); err != nil {
							return err
						}
						rc = resp.Body
					} else {
						gcsPath = strings.TrimPrefix(gcsPath, "gs://")
						bucket, path, _ := strings.Cut(gcsPath, "/")
						fullPath := filepath.Join(path, arch, pkg.Filename())
						log.Printf("downloading gs://%s/%s", bucket, fullPath)

						client, err := storage.NewClient(ctx)
						if err != nil {
							return err
						}
						rc, err = client.Bucket(bucket).Object(fullPath).NewReader(ctx)
						if err != nil {
							return err
						}
					}
					defer rc.Close()

					if err := os.MkdirAll(filepath.Dir(fn), 0755); err != nil {
						return err
					}
					f, err := os.Create(fn)
					if err != nil {
						return err
					}
					defer f.Close()
					if _, err := io.Copy(f, rc); err != nil {
						return err
					}
					log.Printf("wrote %s", fn)
					return nil
				})

				// TODO: Also get (latest) runtime deps here?
			}

			if err := errg.Wait(); err != nil {
				return err
			}

			// Update the local index for all the apks currently in the outDir.
			index.Packages = nil

			if err := filepath.WalkDir(filepath.Join(outDir, arch), func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !strings.HasSuffix(path, ".apk") {
					return nil
				}

				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()
				pkg, err := repository.ParsePackage(f)
				if err != nil {
					return err
				}
				index.Packages = append(index.Packages, pkg)
				return nil
			}); err != nil {
				return err
			}
			fn := filepath.Join(outDir, arch, "APKINDEX.tar.gz")
			log.Printf("writing index: %s (%d total packages)", fn, len(index.Packages))
			f, err := os.Create(fn)
			if err != nil {
				return err
			}
			defer f.Close()
			r, err := repository.ArchiveFromIndex(index)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, r); err != nil {
				return err
			}

			// TODO: Sign index?

			return nil
		},
	}
	cmd.Flags().StringVarP(&outDir, "out-dir", "o", "./packages", "directory to copy packages to")
	cmd.Flags().StringVarP(&indexURL, "index", "i", "https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz", "APKINDEX.tar.gz URL")
	cmd.Flags().BoolVar(&latest, "latest", true, "copy only the latest version of each package")
	cmd.Flags().StringVar(&gcsPath, "gcs", "", "copy objects from a GCS bucket")
	return cmd
}
