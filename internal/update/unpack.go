package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// maxBinary is the biggest program file we unpack.
const maxBinary = maxArchive

// binaryName is the file we take out of the archive.
func binaryName(archiveName string) string {
	if strings.HasSuffix(archiveName, ".zip") {
		return "hotspotpluslight.exe"
	}
	return "hotspotpluslight"
}

// safeEntry is false for an archive entry name that could leave the target folder.
func safeEntry(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// unpackBinary extracts the one program file from the archive at src into dest with mode 0755.
// The archive format follows archiveName (.zip or .tar.gz). Any unsafe entry name makes it fail, even
// when the entry is not the one we want. Other files in the archive (README, LICENSE) are skipped.
// On an error dest does not exist.
func unpackBinary(src, dest, archiveName string) (err error) {
	defer func() {
		if err != nil {
			os.Remove(dest)
		}
	}()
	want := binaryName(archiveName)
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	found, err := extract(src, archiveName, want, out)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && !found {
		err = fmt.Errorf("the archive has no file named %s", want)
	}
	if err == nil {
		err = os.Chmod(dest, 0o755) // OpenFile is limited by the umask
	}
	return err
}

// extract copies the entry named want to w. It reports whether the entry was found.
func extract(src, archiveName, want string, w io.Writer) (bool, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		return extractZip(src, want, w)
	}
	return extractTarGz(src, want, w)
}

// copyLimited copies r to w and fails when r holds more than maxBinary bytes.
func copyLimited(w io.Writer, r io.Reader) error {
	n, err := io.Copy(w, io.LimitReader(r, maxBinary+1))
	if err == nil && n > maxBinary {
		err = errors.New("the program file in the archive is too big")
	}
	return err
}

func extractTarGz(src, want string, w io.Writer) (bool, error) {
	f, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return false, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return found, nil
		}
		if err != nil {
			return false, err
		}
		if !safeEntry(h.Name) {
			return false, fmt.Errorf("unsafe name in the archive: %q", h.Name)
		}
		if path.Clean(h.Name) != want || found {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return false, fmt.Errorf("%s is not a regular file", h.Name)
		}
		if err := copyLimited(w, tr); err != nil {
			return false, err
		}
		found = true
	}
}

func extractZip(src, want string, w io.Writer) (bool, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return false, err
	}
	defer zr.Close()
	found := false
	for _, zf := range zr.File {
		if !safeEntry(zf.Name) {
			return false, fmt.Errorf("unsafe name in the archive: %q", zf.Name)
		}
		if path.Clean(zf.Name) != want || found {
			continue
		}
		if !zf.Mode().IsRegular() {
			return false, fmt.Errorf("%s is not a regular file", zf.Name)
		}
		rc, err := zf.Open()
		if err != nil {
			return false, err
		}
		err = copyLimited(w, rc)
		rc.Close()
		if err != nil {
			return false, err
		}
		found = true
	}
	return found, nil
}
