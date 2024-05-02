package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/jonjohnsonjr/apkrane/internal/version"
	"github.com/spf13/cobra"
	"gitlab.alpinelinux.org/alpine/go/repository"
	"golang.org/x/exp/maps"
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
						return err
					}

					if version.Compare(*got, *parsed) > 0 {
						highest[pkg.Name] = pkg
					}
				}

				packages = maps.Values(highest)
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
