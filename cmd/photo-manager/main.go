package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jolo/photo-manager/internal/frontend"
	"github.com/jolo/photo-manager/internal/importer"
	"github.com/jolo/photo-manager/internal/similarity"
	"github.com/jolo/photo-manager/internal/storage"
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
	root.AddCommand(curateCmd())
	return root
}

func importCmd() *cobra.Command {
	var libRoot string
	var move bool
	var verbose bool
	var curate bool

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

			if curate && result.Imported > 0 {
				fmt.Println()
				return runCurate(libRoot, similarity.DefaultThreshold)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&libRoot, "library", "l", "./library", "Path to the photo library root")
	cmd.Flags().BoolVar(&move, "move", false, "Delete source files after import (default: copy only)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print each file as it's processed")
	cmd.Flags().BoolVar(&curate, "curate", false, "Run similar-photo curation after import")

	return cmd
}

func curateCmd() *cobra.Command {
	var libRoot string
	var threshold int

	cmd := &cobra.Command{
		Use:   "curate",
		Short: "Find similar photos and interactively pick keepers",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCurate(libRoot, threshold)
		},
	}

	cmd.Flags().StringVarP(&libRoot, "library", "l", "./library", "Path to the photo library root")
	cmd.Flags().IntVarP(&threshold, "threshold", "t", similarity.DefaultThreshold,
		"Hamming distance threshold (lower = stricter similarity)")

	return cmd
}

// runCurate is the shared logic for both `curate` and `import --curate`.
func runCurate(libRoot string, threshold int) error {
	idx, err := storage.Load(libRoot)
	if err != nil {
		return fmt.Errorf("loading library index: %w", err)
	}

	pHashes := idx.AllPHashes()
	if len(pHashes) == 0 {
		fmt.Println("No perceptual hashes in library — try re-importing.")
		return nil
	}

	groups := similarity.GroupByHash(pHashes, threshold)
	if len(groups) == 0 {
		fmt.Println("No similar photo groups found.")
		return nil
	}

	fmt.Printf("Found %d group(s) of similar photos.\n", len(groups))

	toDelete, err := frontend.Run(groups, idx.Photos, libRoot)
	if err != nil {
		return fmt.Errorf("curation UI: %w", err)
	}

	if len(toDelete) == 0 {
		fmt.Println("No photos marked for deletion.")
		return nil
	}

	trashDir := filepath.Join(libRoot, ".photo-manager", "trash")
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		return fmt.Errorf("creating trash dir: %w", err)
	}

	trashed := 0
	for _, p := range toDelete {
		dst := filepath.Join(trashDir, filepath.Base(p))
		if err := os.Rename(p, dst); err != nil {
			fmt.Fprintf(os.Stderr, "  error moving %s to trash: %v\n", filepath.Base(p), err)
			continue
		}
		idx.Remove(p)
		fmt.Printf("  trashed: %s\n", filepath.Base(p))
		trashed++
	}

	if err := idx.Save(); err != nil {
		return fmt.Errorf("saving index: %w", err)
	}

	fmt.Printf("\nDone. Trashed %d photo(s). Originals are in %s\n", trashed, trashDir)
	return nil
}
