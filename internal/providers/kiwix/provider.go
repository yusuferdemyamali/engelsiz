// Package kiwix provides the application's sole content provider.
package kiwix

import (
	"context"
	"engelsiz/internal/providers"
	runtime "engelsiz/internal/runtime/kiwix"
)

type Options = runtime.Options

type KiwixProvider struct{ runtime *runtime.Runtime }

var _ providers.ContentProvider = (*KiwixProvider)(nil)

func New(opts Options) (*KiwixProvider, error) {
	r, err := runtime.New(opts)
	if err != nil {
		return nil, err
	}
	return &KiwixProvider{runtime: r}, nil
}
func (p *KiwixProvider) EnsureStarted(ctx context.Context) error { return p.runtime.EnsureStarted(ctx) }
func (p *KiwixProvider) Stop(ctx context.Context) error          { return p.runtime.Stop(ctx) }
func (p *KiwixProvider) Restart(ctx context.Context) error       { return p.runtime.Restart(ctx) }
func (p *KiwixProvider) Status() providers.RuntimeStatus         { return p.runtime.Status() }
