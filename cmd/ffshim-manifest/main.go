// Command ffshim-manifest verifies a built ffshim and records its provenance.
//
// It is used by release CI after the final artifact has been copied into its
// versioned distribution directory. It loads that exact file with the same Go
// loader consumers use, exercises safe shim entry points, and writes a manifest
// that the packaging job verifies again before upload.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/bstkhq/go-ffmpeg-ffi/internal/shim"
)

const manifestSchema = 1

type manifest struct {
	Schema     int    `json:"schema"`
	Platform   string `json:"platform"`
	File       string `json:"file"`
	SHA256     string `json:"sha256"`
	ShimAPI    uint32 `json:"shim_api"`
	ContractID string `json:"contract_id"`
	FFmpeg     ffmpeg `json:"ffmpeg"`
}

type ffmpeg struct {
	Major           int    `json:"major"`
	BuildAVUtil     uint32 `json:"build_avutil_major"`
	BuildAVCodec    uint32 `json:"build_avcodec_major"`
	BuildAVFormat   uint32 `json:"build_avformat_major"`
	RuntimeAVUtil   uint32 `json:"runtime_avutil"`
	RuntimeAVCodec  uint32 `json:"runtime_avcodec"`
	RuntimeAVFormat uint32 `json:"runtime_avformat"`
}

func main() {
	shimDir := flag.String("shim-dir", "", "directory containing the shim to verify")
	output := flag.String("output", "", "manifest output path (default: stdout)")
	field := flag.String("field", "", "print one field: ffmpeg-major")
	verify := flag.String("verify", "", "verify an existing manifest without loading it")
	flag.Parse()

	if *verify != "" {
		if err := verifyManifest(*verify); err != nil {
			fatal(err)
		}
		return
	}
	if *shimDir == "" {
		fatal(errors.New("-shim-dir is required"))
	}
	if err := os.Setenv("FFMPEG_SHIM_DIR", *shimDir); err != nil {
		fatal(err)
	}

	m, err := inspect()
	if err != nil {
		fatal(err)
	}
	if *field != "" {
		switch *field {
		case "ffmpeg-major":
			fmt.Println(m.FFmpeg.Major)
			return
		default:
			fatal(fmt.Errorf("unknown field %q", *field))
		}
	}
	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fatal(err)
	}
	encoded = append(encoded, '\n')
	if *output == "" {
		_, err = os.Stdout.Write(encoded)
	} else {
		err = os.WriteFile(*output, encoded, 0o644)
	}
	if err != nil {
		fatal(err)
	}
}

func inspect() (manifest, error) {
	if err := shim.Load(); err != nil {
		return manifest{}, fmt.Errorf("load shim: %w", err)
	}
	if !shim.IsLoaded() {
		return manifest{}, fmt.Errorf("configured shim was not loaded: %v", shim.LoadError())
	}
	info := shim.Info()
	if info.API != shim.APIVersion || info.ContractID != shim.ContractID {
		return manifest{}, fmt.Errorf("loaded shim contract mismatch: %+v", info)
	}
	if err := shim.SetLogLevel(32); err != nil {
		return manifest{}, fmt.Errorf("call ffshim_log_set_level: %w", err)
	}
	if _, _, _, _, err := shim.AVFrameColorOffsets(); err != nil {
		return manifest{}, fmt.Errorf("call ffshim_avframe_color_offsets: %w", err)
	}
	path := shim.Path()
	digest, err := fileSHA256(path)
	if err != nil {
		return manifest{}, err
	}
	return manifest{
		Schema:     manifestSchema,
		Platform:   runtime.GOOS + "/" + runtime.GOARCH,
		File:       filepath.Base(path),
		SHA256:     digest,
		ShimAPI:    info.API,
		ContractID: fmt.Sprintf("0x%016x", info.ContractID),
		FFmpeg: ffmpeg{
			Major:           info.FFmpegMajor,
			BuildAVUtil:     info.BuildAVUtilMajor,
			BuildAVCodec:    info.BuildAVCodecMajor,
			BuildAVFormat:   info.BuildAVFormatMajor,
			RuntimeAVUtil:   info.RuntimeAVUtil,
			RuntimeAVCodec:  info.RuntimeAVCodec,
			RuntimeAVFormat: info.RuntimeAVFormat,
		},
	}, nil
}

func verifyManifest(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m manifest
	if err := json.Unmarshal(contents, &m); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if m.Schema != manifestSchema {
		return fmt.Errorf("manifest schema %d, expected %d", m.Schema, manifestSchema)
	}
	if m.Platform == "" || m.File == "" || m.FFmpeg.Major == 0 {
		return errors.New("manifest is missing platform, file, or FFmpeg family")
	}
	if filepath.Base(m.File) != m.File {
		return fmt.Errorf("manifest file must be a basename, got %q", m.File)
	}
	if m.ShimAPI != shim.APIVersion {
		return fmt.Errorf("manifest shim API %d, expected %d", m.ShimAPI, shim.APIVersion)
	}
	if m.ContractID != fmt.Sprintf("0x%016x", shim.ContractID) {
		return fmt.Errorf("manifest contract %s, expected 0x%016x", m.ContractID, shim.ContractID)
	}
	digest, err := fileSHA256(filepath.Join(filepath.Dir(path), m.File))
	if err != nil {
		return err
	}
	if m.SHA256 != digest {
		return fmt.Errorf("manifest checksum %s does not match %s", m.SHA256, m.File)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "ffshim-manifest:", err)
	os.Exit(1)
}
