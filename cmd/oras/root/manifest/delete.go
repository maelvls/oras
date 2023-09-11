/*
Copyright The ORAS Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package manifest

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote/auth"
	oerrors "oras.land/oras/cmd/oras/internal/errors"
	"oras.land/oras/cmd/oras/internal/option"
	"oras.land/oras/internal/registryutil"
)

type deleteOptions struct {
	option.Common
	option.Confirmation
	option.Descriptor
	option.Pretty
	option.Remote

	targetRef string
}

func deleteCmd() *cobra.Command {
	var opts deleteOptions
	cmd := &cobra.Command{
		Use:     "delete [flags] <name>{:<tag>|@<digest>}",
		Aliases: []string{"remove", "rm"},
		Short:   "Delete a manifest from remote registry",
		Long: `Delete a manifest from remote registry

Example - Delete a manifest tagged with 'v1' from repository 'localhost:5000/hello':
  oras manifest delete localhost:5000/hello:v1

Example - Delete a manifest without prompting confirmation:
  oras manifest delete --force localhost:5000/hello:v1

Example - Delete a manifest and print its descriptor:
  oras manifest delete --descriptor localhost:5000/hello:v1

Example - Delete a manifest by digest 'sha256:99e4703fbf30916f549cd6bfa9cdbab614b5392fbe64fdee971359a77073cdf9' from repository 'localhost:5000/hello':
  oras manifest delete localhost:5000/hello@sha:99e4703fbf30916f549cd6bfa9cdbab614b5392fbe64fdee971359a77073cdf9
`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if opts.OutputDescriptor && !opts.Force {
				return errors.New("must apply --force to confirm the deletion if the descriptor is outputted")
			}
			return option.Parse(&opts)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.targetRef = args[0]
			return deleteManifest(cmd.Context(), opts)
		},
	}

	opts.EnableDistributionSpecFlag()
	option.ApplyFlags(&opts, cmd.Flags())
	return cmd
}

func deleteManifest(ctx context.Context, opts deleteOptions) error {
	ctx, logger := opts.WithContext(ctx)
	repo, err := opts.NewRepository(opts.targetRef, opts.Common, logger)
	if err != nil {
		return err
	}

	if repo.Reference.Reference == "" {
		return oerrors.NewErrInvalidReference(repo.Reference)
	}

	// add both pull, push and delete scope hints for dst repository to save potential delete-scope token requests during deleting
	hints := []string{auth.ActionPull, auth.ActionPush, auth.ActionDelete}
	if opts.ReferrersAPI == nil || !*opts.ReferrersAPI {
		// possiblely need pushing to add a new referrers index
		hints = append(hints, auth.ActionPush)
	}
	ctx = registryutil.WithScopeHint(repo, ctx, hints...)
	manifests := repo.Manifests()
	desc, err := manifests.Resolve(ctx, opts.targetRef)
	if err != nil {
		if errors.Is(err, errdef.ErrNotFound) {
			if opts.Force && !opts.OutputDescriptor {
				// ignore nonexistent
				fmt.Println("Missing", opts.targetRef)
				return nil
			}
			return fmt.Errorf("%s: the specified manifest does not exist", opts.targetRef)
		}
		return err
	}

	prompt := fmt.Sprintf("Are you sure you want to delete the manifest %q and all tags associated with it?", desc.Digest)
	confirmed, err := opts.AskForConfirmation(os.Stdin, prompt)
	if err != nil {
		return err
	}
	if !confirmed {
		return nil
	}

	if err = manifests.Delete(ctx, desc); err != nil {
		return fmt.Errorf("failed to delete %s: %w", opts.targetRef, err)
	}

	if opts.OutputDescriptor {
		descJSON, err := opts.Marshal(desc)
		if err != nil {
			return err
		}
		return opts.Output(os.Stdout, descJSON)
	}

	fmt.Println("Deleted", opts.targetRef)

	return nil
}
