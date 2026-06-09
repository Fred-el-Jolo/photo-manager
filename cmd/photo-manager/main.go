package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jolo/photo-manager/internal/api"
	"github.com/jolo/photo-manager/internal/importer"
	"github.com/jolo/photo-manager/internal/scanner"
	"github.com/jolo/photo-manager/internal/session"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "photo-manager",
		Short: "Import, deduplicate, and organize your photo library",
	}
	root.AddCommand(importCmd())
	root.AddCommand(scanCmd())
	root.AddCommand(serveCmd())
	return root
}

func importCmd() *cobra.Command {
	var libRoot string
	var move bool
	var verbose bool

	cmd := &cobra.Command{
		Use:   "import <source-dir>",
		Short: "Import photos from a source directory into the library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := importer.Run(importer.Options{
				SourceDir: args[0],
				LibRoot:   libRoot,
				Move:      move,
				Verbose:   verbose,
			})
			if err != nil {
				return err
			}
			fmt.Printf("\nDone. imported=%d  duplicates=%d  errors=%d\n",
				result.Imported, result.Duplicates, result.Errors)
			return nil
		},
	}

	cmd.Flags().StringVarP(&libRoot, "library", "l", "./library", "Path to the photo library root")
	cmd.Flags().BoolVar(&move, "move", false, "Delete source files after import (default: copy only)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print each file as it's processed")

	return cmd
}

func scanCmd() *cobra.Command {
	var inputDir string
	var outputDir string
	var threshold int
	var limit int
	var port int
	var noWindow bool
	var staticDir string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan an input directory, build a curation session, and serve it",
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, err := scanner.Scan(inputDir, outputDir, threshold, limit, func(done, total int) {
				if verbose {
					fmt.Printf("\r  scanning %d/%d", done, total)
				}
			})
			if err != nil {
				return err
			}
			if err := sess.Save(); err != nil {
				return err
			}
			fmt.Printf("\nScanned %d months, %d groups\n", len(sess.Months), totalGroups(sess))
			return runServe(sess, port, staticDir, noWindow)
		},
	}

	cmd.Flags().StringVar(&inputDir, "input", "", "Input directory to scan")
	cmd.Flags().StringVar(&outputDir, "output", "", "Output directory for the session")
	cmd.Flags().IntVar(&threshold, "threshold", 10, "Hamming distance threshold for grouping")
	cmd.Flags().IntVarP(&limit, "limit", "N", 0, "Max photos to scan (0 = all; useful for dev)")
	cmd.Flags().IntVar(&port, "port", 8080, "Port to serve the curation UI on")
	cmd.Flags().BoolVar(&noWindow, "no-window", false, "Do not launch a desktop window")
	cmd.Flags().StringVar(&staticDir, "static", defaultStaticDir(), "Directory of static frontend assets")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print scan progress")

	return cmd
}

func serveCmd() *cobra.Command {
	var outputDir string
	var port int
	var noWindow bool
	var staticDir string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve an existing curation session",
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, err := session.Load(outputDir)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					fmt.Println("No session found. Run `scan` first.")
					return nil
				}
				return err
			}
			return runServe(sess, port, staticDir, noWindow)
		},
	}

	cmd.Flags().StringVar(&outputDir, "output", "", "Output directory containing the session")
	cmd.Flags().IntVar(&port, "port", 8080, "Port to serve the curation UI on")
	cmd.Flags().BoolVar(&noWindow, "no-window", false, "Do not launch a desktop window")
	cmd.Flags().StringVar(&staticDir, "static", defaultStaticDir(), "Directory of static frontend assets")

	return cmd
}

// runServe starts the HTTP server for the given session and optionally opens a window.
func runServe(sess *session.Session, port int, staticDir string, noWindow bool) error {
	handler := api.New(sess, staticDir)
	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{Addr: addr, Handler: handler}

	if !noWindow {
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = launchWindow(port)
		}()
	}

	fmt.Printf("Serving on http://localhost:%d\n", port)
	return srv.ListenAndServe()
}

// launchWindow opens the Electrobun desktop window pointed at the local server.
// Best-effort: if it fails, fall back to printing the URL.
func launchWindow(port int) error {
	cmd := exec.Command("bun", "run", "--cwd", "frontend", "dev")
	cmd.Env = append(os.Environ(), "PM_PORT="+strconv.Itoa(port))
	if err := cmd.Start(); err != nil {
		fmt.Printf("  (open http://localhost:%d in a browser)\n", port)
		return nil
	}
	return nil
}

func totalGroups(sess *session.Session) int {
	n := 0
	for _, m := range sess.Months {
		n += len(m.Groups)
	}
	return n
}

// defaultStaticDir returns a path to frontend/dist relative to the executable,
// falling back to a repo-relative path when os.Executable fails (e.g. in tests).
func defaultStaticDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "frontend", "dist")
	}
	return "frontend/dist"
}
