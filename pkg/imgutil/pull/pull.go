/*
   Copyright The containerd Authors.

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

// Package pull forked from https://github.com/containerd/containerd/blob/v1.4.3/cmd/ctr/commands/content/fetch.go
package pull

import (
	"context"
	"fmt"
	"io"

	"github.com/awslabs/soci-snapshotter/fs/source"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/log"
	ctdsnapshotters "github.com/containerd/containerd/pkg/snapshotters"
	"github.com/containerd/containerd/remotes"
	"github.com/containerd/nerdctl/pkg/imgutil/jobs"
	"github.com/containerd/nerdctl/pkg/platformutil"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Config for content fetch
type Config struct {
	// Resolver
	Resolver remotes.Resolver
	// ProgressOutput to display progress
	ProgressOutput io.Writer
	// RemoteOpts, e.g. containerd.WithPullUnpack.
	//
	// Regardless to RemoteOpts, the following opts are always set:
	// WithResolver, WithImageHandler, WithSchema1Conversion
	//
	// RemoteOpts related to unpacking can be set only when len(Platforms) is 1.
	RemoteOpts []containerd.RemoteOpt
	Platforms  []ocispec.Platform // empty for all-platforms
}

// Pull loads all resources into the content store and returns the image
func Pull(ctx context.Context, client *containerd.Client, ref string, config *Config) (containerd.Image, error) {
	ongoing := jobs.New(ref)

	pctx, stopProgress := context.WithCancel(ctx)
	progress := make(chan struct{})

	go func() {
		if config.ProgressOutput != nil {
			// no progress bar, because it hides some debug logs
			jobs.ShowProgress(pctx, ongoing, client.ContentStore(), config.ProgressOutput)
		}
		close(progress)
	}()

	/* soci uses a diff h so i had to comment the nerdctl default oen out || h := images.HandlerFunc(func(ctx context.Context, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
		if desc.MediaType != images.MediaTypeDockerSchema1Manifest {
			ongoing.Add(desc)
		}
		return nil, nil
	}) */

	/*adding options for a docker.Newresolver() call to get SOCI resolver
	var PushTracker = docker.NewInMemoryTracker()

	options := docker.ResolverOptions{
		Tracker: PushTracker,
	}

	hostOptions := dockerconfig.HostOptions{}
	hostOptions.Credentials = func(host string) (string, string, error) {
		// If host doesn't match...
		// Only one host
	}
	hostOptions.DefaultTLS = &tls.Config{}

	options.Hosts = dockerconfig.ConfigureHosts(ctx, hostOptions)

	docker.NewResolver(options)*/

	log.G(pctx).WithField("image", ref).Debug("fetching")
	platformMC := platformutil.NewMatchComparerFromOCISpecPlatformSlice(config.Platforms)
	opts := []containerd.RemoteOpt{
		/*containerd.WithResolver(config.Resolver),
		containerd.WithImageHandler(h),
		//nolint:staticcheck
		containerd.WithSchema1Conversion, //lint:ignore SA1019 nerdctl should support schema1 as well.
		containerd.WithPlatformMatcher(platformMC),*/

		containerd.WithPullLabels(make(map[string]string, 0)), //soci
		containerd.WithResolver(config.Resolver),
		/*containerd.WithImageHandler(images.HandlerFunc(func(ctx context.Context, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
			if desc.MediaType != images.MediaTypeDockerSchema1Manifest {
				fmt.Printf("fetching %v... %v\n", desc.Digest.String()[:15], desc.MediaType)
			}
			return nil, nil
		})),*/
		//nolint:staticcheck
		containerd.WithSchema1Conversion,       //lint:ignore SA1019 nerdctl should support schema1 as well.
		containerd.WithPullUnpack,              //soci
		containerd.WithPlatform(""),            //soci
		containerd.WithPullSnapshotter("soci"), //soci
		containerd.WithImageHandlerWrapper(source.AppendDefaultLabelsHandlerWrapper("", ctdsnapshotters.AppendInfoHandlerWrapper(ref))), //soci
		//containerd.WithPlatformMatcher(platformMC),
	}
	//opts = append(opts, config.RemoteOpts...)

	var (
		img containerd.Image
		err error
	)
	if len(config.Platforms) == 1 {
		// client.Pull is for single-platform (w/ unpacking)
		fmt.Println("CALLING CONTAINERD PULL")
		fmt.Println("reference: " + ref)
		img, err = client.Pull(pctx, ref, opts...)
		fmt.Println("FINISHED CONTAINERD PULL")
	} else {
		// client.Fetch is for multi-platform (w/o unpacking)
		var imagesImg images.Image
		imagesImg, err = client.Fetch(pctx, ref, opts...)
		img = containerd.NewImageWithPlatform(client, imagesImg, platformMC)
	}
	fmt.Println("StOPPED PROGRESS???")
	stopProgress()
	fmt.Println("StOPPED PROGRESS?")
	if err != nil {
		fmt.Println("Returning error")
		return nil, err
	}

	<-progress
	return img, nil
}
