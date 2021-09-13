package oras

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	orascontent "github.com/deislabs/oras/pkg/content"

	"github.com/containerd/containerd/images"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	artifactspec "github.com/oras-project/artifacts-spec/specs-go/v1"
	"github.com/pkg/errors"
)

type pushOpts struct {
	config              *ocispec.Descriptor
	configMediaType     string
	configAnnotations   map[string]string
	manifest            *ocispec.Descriptor
	manifestAnnotations map[string]string
	manifestWriter      io.Writer
	validateName        func(desc ocispec.Descriptor) error
	baseHandlers        []images.Handler
	artifact            *artifactspec.Manifest
}

func pushOptsDefaults() *pushOpts {
	return &pushOpts{
		validateName: ValidateNameAsPath,
	}
}

// PushOpt allows callers to set options on the oras push
type PushOpt func(o *pushOpts) error

// WithConfig overrides the config - setting this will ignore WithConfigMediaType and WithConfigAnnotations
func WithConfig(config ocispec.Descriptor) PushOpt {
	return func(o *pushOpts) error {
		o.config = &config
		return nil
	}
}

// WithConfigMediaType overrides the config media type
func WithConfigMediaType(mediaType string) PushOpt {
	return func(o *pushOpts) error {
		o.configMediaType = mediaType
		return nil
	}
}

// WithConfigAnnotations overrides the config annotations
func WithConfigAnnotations(annotations map[string]string) PushOpt {
	return func(o *pushOpts) error {
		o.configAnnotations = annotations
		return nil
	}
}

// WithManifest overrides the manifest - setting this will ignore WithManifestConfigAnnotations
func WithManifest(manifest ocispec.Descriptor) PushOpt {
	return func(o *pushOpts) error {
		o.manifest = &manifest
		return nil
	}
}

// WithManifestAnnotations overrides the manifest annotations
func WithManifestAnnotations(annotations map[string]string) PushOpt {
	return func(o *pushOpts) error {
		o.manifestAnnotations = annotations
		return nil
	}
}

// WithManifestWriter exports the pushed manifest
func WithManifestWriter(writer io.Writer) PushOpt {
	return func(o *pushOpts) error {
		o.manifestWriter = writer
		return nil
	}
}

// WithNameValidation validates the image title in the descriptor.
// Pass nil to disable name validation.
func WithNameValidation(validate func(desc ocispec.Descriptor) error) PushOpt {
	return func(o *pushOpts) error {
		o.validateName = validate
		return nil
	}
}

// ValidateNameAsPath validates name in the descriptor as file path in order
// to generate good packages intended to be pulled using the FileStore or
// the oras cli.
// For cross-platform considerations, only unix paths are accepted.
func ValidateNameAsPath(desc ocispec.Descriptor) error {
	// no empty name
	path, ok := orascontent.ResolveName(desc)
	if !ok || path == "" {
		return orascontent.ErrNoName
	}

	// path should be clean
	if target := filepath.ToSlash(filepath.Clean(path)); target != path {
		return errors.Wrap(ErrDirtyPath, path)
	}

	// path should be slash-separated
	if strings.Contains(path, "\\") {
		return errors.Wrap(ErrPathNotSlashSeparated, path)
	}

	// disallow absolute path: covers unix and windows format
	if strings.HasPrefix(path, "/") {
		return errors.Wrap(ErrAbsolutePathDisallowed, path)
	}
	if len(path) > 2 {
		c := path[0]
		if path[1] == ':' && path[2] == '/' && ('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
			return errors.Wrap(ErrAbsolutePathDisallowed, path)
		}
	}

	// disallow path traversal
	if strings.HasPrefix(path, "../") || path == ".." {
		return errors.Wrap(ErrPathTraversalDisallowed, path)
	}

	return nil
}

// WithPushBaseHandler provides base handlers, which will be called before
// any push specific handlers.
func WithPushBaseHandler(handlers ...images.Handler) PushOpt {
	return func(o *pushOpts) error {
		o.baseHandlers = append(o.baseHandlers, handlers...)
		return nil
	}
}

// WithPushStatusTrack report results to a provided writer
func WithPushStatusTrack(writer io.Writer) PushOpt {
	return WithPushBaseHandler(pushStatusTrack(writer))
}

func pushStatusTrack(writer io.Writer) images.Handler {
	var printLock sync.Mutex
	return images.HandlerFunc(func(ctx context.Context, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
		if name, ok := orascontent.ResolveName(desc); ok {
			printLock.Lock()
			defer printLock.Unlock()
			fmt.Fprintln(writer, "Uploading", desc.Digest.Encoded()[:12], name)
		}
		return nil, nil
	})
}

// AsArtifact set oras to push contents as an artifact
func AsArtifact(artifactType string, manifest ocispec.Descriptor) PushOpt {
	return func(o *pushOpts) error {
		o.artifact = &artifactspec.Manifest{
			ArtifactType: artifactType,
			Subject:      convertV1DescriptorToV2(manifest),
		}
		return nil
	}
}