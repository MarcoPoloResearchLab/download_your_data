package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/normalize-instruction-pngs <image> [...]")
		os.Exit(2)
	}
	for _, path := range os.Args[1:] {
		if err := normalizePNG(path); err != nil {
			fmt.Fprintf(os.Stderr, "normalize %s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

func normalizePNG(path string) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	decoded, _, decodeErr := image.Decode(source)
	closeErr := source.Close()
	if decodeErr != nil {
		return decodeErr
	}
	if closeErr != nil {
		return closeErr
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	output, err := os.CreateTemp(filepath.Dir(path), ".instruction-*.png")
	if err != nil {
		return err
	}
	outputPath := output.Name()
	keepOutput := false
	defer func() {
		if !keepOutput {
			_ = os.Remove(outputPath)
		}
	}()

	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(output, decoded); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	if err := os.Chmod(outputPath, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Rename(outputPath, path); err != nil {
		return err
	}
	keepOutput = true
	return nil
}
