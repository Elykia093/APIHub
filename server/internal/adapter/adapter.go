package adapter

import (
	"context"
	"fmt"
	"sync"

	"github.com/elykia/apihub/server/ent"
	"github.com/elykia/apihub/server/internal/apperror"
	"github.com/elykia/apihub/server/internal/cryptoutil"
	"github.com/elykia/apihub/server/internal/domain"
	"github.com/elykia/apihub/server/internal/netclient"
)

type Adapter interface {
	Descriptor() domain.AdapterDescriptor
	CheckIn(context.Context, domain.SiteContext, string) (domain.CheckinResult, error)
	FetchAnnouncements(context.Context, domain.SiteContext) (domain.AnnouncementResult, error)
}

type Registry struct {
	adapters map[domain.AdapterName]Adapter
	vault    *cryptoutil.Vault
}

func NewRegistry(vault *cryptoutil.Vault, adapters ...Adapter) *Registry {
	registry := &Registry{adapters: make(map[domain.AdapterName]Adapter), vault: vault}
	for _, item := range adapters {
		registry.adapters[item.Descriptor().Name] = item
	}
	return registry
}

func (r *Registry) Get(name domain.AdapterName) (Adapter, error) {
	item, ok := r.adapters[name]
	if !ok {
		return nil, apperror.New(422, apperror.ValidationError, fmt.Sprintf("Unsupported site adapter: %s", name), false)
	}
	return item, nil
}

func (r *Registry) List() []domain.AdapterDescriptor {
	order := []domain.AdapterName{domain.NewAPI, domain.Sub2API, domain.ZenAPI}
	result := make([]domain.AdapterDescriptor, 0, len(order))
	for _, name := range order {
		if item, ok := r.adapters[name]; ok {
			result = append(result, item.Descriptor())
		}
	}
	return result
}

func (r *Registry) Context(site *ent.Site) (domain.SiteContext, error) {
	token, err := r.vault.Decrypt(site.AccessTokenCiphertext)
	if err != nil {
		return domain.SiteContext{}, fmt.Errorf("decrypt site credential: %w", err)
	}
	return domain.SiteContext{BaseURL: site.BaseURL, UserID: site.UserID, AccessToken: token, Timezone: site.Timezone}, nil
}

type Detector struct{ http *netclient.Client }

func NewDetector(http *netclient.Client) *Detector { return &Detector{http: http} }

type detectionResult struct {
	response netclient.Response
	err      error
}

func (d *Detector) Detect(ctx context.Context, baseURL string) (domain.AdapterName, error) {
	paths := []string{"/api/public/site-info", "/api/v1/auth/me", "/api/status"}
	results := make([]detectionResult, len(paths))
	var wait sync.WaitGroup
	for index, path := range paths {
		wait.Add(1)
		go func() {
			defer wait.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					results[index].err = fmt.Errorf("adapter detection panic: %v", recovered)
				}
			}()
			results[index].response, results[index].err = d.http.RequestJSON(ctx, baseURL, path, "GET", nil, nil)
		}()
	}
	wait.Wait()
	if results[0].err == nil && looksLikeZen(results[0].response.JSON) {
		return domain.ZenAPI, nil
	}
	if results[1].err == nil && looksLikeSub2(results[1].response.JSON) {
		return domain.Sub2API, nil
	}
	if results[2].err == nil && looksLikeNew(results[2].response.JSON) {
		return domain.NewAPI, nil
	}
	if results[0].err != nil && results[1].err != nil && results[2].err != nil {
		return "", results[0].err
	}
	return "", apperror.New(422, apperror.ValidationError, "Site type could not be detected; select a concrete adapter", false)
}

func looksLikeZen(value any) bool {
	data, ok := record(value)
	if !ok {
		return false
	}
	if nested, ok := record(data["data"]); ok {
		data = nested
	}
	return hasAny(data, "site_mode", "registration_mode", "linuxdo_enabled")
}
func looksLikeSub2(value any) bool {
	data, ok := record(value)
	return ok && has(data, "code") && hasAny(data, "message", "data")
}
func looksLikeNew(value any) bool {
	data, ok := record(value)
	if !ok {
		return false
	}
	_, boolean := data["success"].(bool)
	_, nested := record(data["data"])
	return boolean || nested
}
