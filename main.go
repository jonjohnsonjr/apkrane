package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
	var repoAlias, outDir string
	var archs []string
	cmd := &cobra.Command{
		Use:     "cp",
		Aliases: []string{"copy"},
		RunE: func(cmd *cobra.Command, args []string) error {
			wantSet := map[string]struct{}{}
			for _, p := range args {
				wantSet[p] = struct{}{}
			}

			errg, ctx := errgroup.WithContext(cmd.Context())

			for _, arch := range archs {
				repoURL := repoURL(repoAlias, arch)

				indexURL := repoURL + "/APKINDEX.tar.gz"
				in, err := fetchIndex(ctx, indexURL)
				if err != nil {
					return fmt.Errorf("fetching %q: %w", indexURL, err)
				}
				defer in.Close()
				index, err := repository.IndexFromArchive(io.NopCloser(in))
				if err != nil {
					return fmt.Errorf("parsing %q: %w", indexURL, err)
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

				log.Printf("downloading %d packages for %s", len(packages), arch)

				for _, pkg := range packages {
					pkg := pkg
					errg.Go(func() error {
						fn := filepath.Join(outDir, arch, pkg.Filename())
						if _, err := os.Stat(fn); err == nil {
							log.Printf("skipping %s: already exists", fn)
							return nil
						}

						url := fmt.Sprintf("%s/%s", repoURL, pkg.Filename())
						req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
						if err != nil {
							return err
						}
						resp, err := http.DefaultClient.Do(req)
						if err != nil {
							return err
						}
						defer resp.Body.Close()

						if err := os.MkdirAll(filepath.Join(outDir, arch), 0755); err != nil {
							return err
						}

						f, err := os.Create(fn)
						if err != nil {
							return err
						}
						defer f.Close()

						log.Println("downloading", url)
						if _, err := io.Copy(f, resp.Body); err != nil {
							return err
						}
						log.Printf("wrote %s", fn)
						return nil
					})

					// TODO: Also get (latest) runtime deps here?
				}
			}

			if err := errg.Wait(); err != nil {
				return err
			}

			// TODO: Update the local index, to include all the new and existing packages.

			return nil
		},
	}
	cmd.Flags().StringVarP(&outDir, "out-dir", "o", "./packages", "directory to copy packages to")
	cmd.Flags().StringVarP(&repoAlias, "repo", "r", "wolfi", "repository alias or URL")
	cmd.Flags().BoolVar(&latest, "latest", true, "copy only the latest version of each package")
	cmd.Flags().StringSliceVar(&archs, "arch", []string{"x86_64", "aarch64"}, "copy only packages with the given arches")
	return cmd
}

func repoURL(alias, arch string) string {
	if strings.HasPrefix(alias, "https://") {
		return alias
	}

	switch alias {
	case "wolfi":
		return fmt.Sprintf("https://packages.wolfi.dev/os/%s", arch)
	case "extras", "extra-packages":
		return fmt.Sprintf("https://packages.cgr.dev/extras/%s", arch)
	case "enterprise", "enterprise-packages":
		return fmt.Sprintf("https://packages.cgr.dev/os/%s", arch)
	}
	log.Fatalf("unknown repo alias %q", alias)
	return ""
}
