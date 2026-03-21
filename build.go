//go:build ignore

package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	cmd := "build"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "build":
		if err := build(); err != nil {
			fmt.Fprintf(os.Stderr, "build failed: %v\n", err)
			os.Exit(1)
		}
	case "clean":
		if err := clean(); err != nil {
			fmt.Fprintf(os.Stderr, "clean failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\nusage: go run build.go [build|clean]\n", cmd)
		os.Exit(1)
	}
}

func build() error {
	// Build frontend
	fmt.Println("Building frontend...")
	npmBuild := exec.Command("npm", "run", "build")
	npmBuild.Dir = "frontend"
	npmBuild.Stdout = os.Stdout
	npmBuild.Stderr = os.Stderr
	if err := npmBuild.Run(); err != nil {
		return fmt.Errorf("npm run build: %w", err)
	}

	// Copy frontend dist to embed dir
	fmt.Println("Copying frontend dist...")
	embedDir := filepath.Join("backend", "internal", "frontend", "dist")
	if err := removeContents(embedDir); err != nil {
		return fmt.Errorf("clearing embed dir: %w", err)
	}
	srcDir := filepath.Join("frontend", "dist")
	if err := copyDir(srcDir, embedDir); err != nil {
		return fmt.Errorf("copying dist: %w", err)
	}

	// Build backend
	fmt.Println("Building backend...")
	binary := "server"
	if runtime.GOOS == "windows" {
		binary = "server.exe"
	}
	goBuild := exec.Command("go", "build", "-buildvcs=false", "-o", binary, "./cmd/server")
	goBuild.Dir = "backend"
	goBuild.Stdout = os.Stdout
	goBuild.Stderr = os.Stderr
	if err := goBuild.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}

	fmt.Println("Build complete.")
	return nil
}

func clean() error {
	dirs := []string{
		filepath.Join("frontend", "dist"),
	}
	for _, d := range dirs {
		fmt.Printf("Removing %s\n", d)
		os.RemoveAll(d)
	}

	embedDir := filepath.Join("backend", "internal", "frontend", "dist")
	fmt.Printf("Clearing %s\n", embedDir)
	if err := removeContents(embedDir); err != nil {
		return err
	}

	gitkeep := filepath.Join(embedDir, ".gitkeep")
	if err := os.WriteFile(gitkeep, nil, 0644); err != nil {
		return fmt.Errorf("restoring .gitkeep: %w", err)
	}

	serverBinary := "server"
	if runtime.GOOS == "windows" {
		serverBinary = "server.exe"
	}
	serverPath := filepath.Join("backend", serverBinary)
	fmt.Printf("Removing %s\n", serverPath)
	os.Remove(serverPath)

	fmt.Println("Clean complete.")
	return nil
}

// removeContents deletes everything inside dir but keeps dir itself.
func removeContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dir, 0755)
		}
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// copyDir recursively copies src to dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
