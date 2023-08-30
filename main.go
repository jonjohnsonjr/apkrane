package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"gitlab.alpinelinux.org/alpine/go/repository"
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

func ls() *cobra.Command {
	var full bool
	var j bool

	cmd := &cobra.Command{
		Use:     "ls",
		Example: `apkrane ls https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			u := args[0]
			if scheme, _, ok := strings.Cut(u, "://"); ok && strings.HasPrefix(scheme, "http") {
				return httpIndex(cmd.OutOrStdout(), u, full, j)
			}

			return fmt.Errorf("todo: handle filepaths")
		},
	}

	cmd.Flags().BoolVar(&full, "full", false, "print the full url or path")
	cmd.Flags().BoolVar(&j, "json", false, "print each package as json")

	return cmd
}

func httpIndex(w io.Writer, u string, full bool, j bool) error {
	resp, err := http.Get(u)
	if err != nil {
		return fmt.Errorf("GET %q: %w", u, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("GET %q: status %d", u, resp.StatusCode)
	}

	index, err := repository.IndexFromArchive(io.NopCloser(bufio.NewReaderSize(resp.Body, 1<<20)))
	if err != nil {
		return fmt.Errorf("parsing %q: %w", u, err)
	}

	dir := strings.TrimSuffix(u, "/APKINDEX.tar.gz")

	var enc *json.Encoder
	if j {
		enc = json.NewEncoder(w)
	}
	for _, pkg := range index.Packages {
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
}
