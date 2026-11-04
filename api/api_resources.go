package api

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"golang.org/x/crypto/ssh"
	"subuk/vmango/compute"
)

type apiVolumeInput struct {
	Cloud  string `path:"cloud"`
	Volume string `path:"volume"`
}
type apiVolumeDeleteInput struct {
	Cloud string `path:"cloud"`
	SubmissionInput
	Volume string `path:"volume"`
}
type apiVolumeCreate struct {
	Name         string `json:"name" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$"`
	StorageClass string `json:"storage_class" minLength:"1"`
	SizeBytes    uint64 `json:"size_bytes" minimum:"1"`
}
type apiVolumeCloneInput struct {
	Cloud string `path:"cloud"`
	SubmissionInput
	Volume string `path:"volume"`
	Body   apiVolumeCreate
}
type apiVolumeResizeInput struct {
	Cloud string `path:"cloud"`
	SubmissionInput
	Volume string `path:"volume"`
	Body   struct {
		SizeBytes uint64 `json:"size_bytes" minimum:"1"`
	}
}

func (e *Environ) registerVolumeOperations(api huma.API) {
	const path = "/clouds/{cloud}/volumes/{volume}"
	registerAPI(api, "GET", path, "get-volume", "Get volume and applicable clone choices", func(ctx context.Context, in *apiVolumeInput) (*apiResult[apiVolumeDetail], error) {
		v, err := e.getWorkloadVolume(ctx, in.Cloud, in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		out := apiVolumeDetail{APIVolumeFields: e.volumeDTO(v)}
		if choices, err := e.clouds.VolumeCatalog(in.Cloud, v); err == nil {
			out.Catalog = &apiVolumeCatalog{StorageClasses: choices}
		}
		return &apiResult[apiVolumeDetail]{Body: out}, nil
	})
	registerAPI(api, "POST", path+"/clone", "clone-volume", "Queue volume cloning", func(ctx context.Context, in *apiVolumeCloneInput) (*apiAccepted, error) {
		v, err := e.clouds.VolumeAddress(in.Cloud, in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		b := in.Body
		p := compute.VolumeCloneParams{NodeId: v.Host, OriginalPath: v.Path, NewName: b.Name, NewSize: compute.NewSize(b.SizeBytes, compute.SizeUnitB)}
		return e.submitTask(ctx, in.IdempotencyKey, "volume.clone", compute.InfrastructureCommand{CloudID: in.Cloud, Node: v.Host, StorageClass: b.StorageClass, Path: v.Path, CloneVolume: &p})
	})
	registerAPI(api, "POST", path+"/resize", "resize-volume", "Queue volume growth", func(ctx context.Context, in *apiVolumeResizeInput) (*apiAccepted, error) {
		v, err := e.clouds.VolumeAddress(in.Cloud, in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		return e.submitTask(ctx, in.IdempotencyKey, "volume.resize", compute.InfrastructureCommand{CloudID: in.Cloud, Node: v.Host, Path: v.Path, Size: compute.NewSize(in.Body.SizeBytes, compute.SizeUnitB)})
	})
	registerAPI(api, "DELETE", path, "delete-volume", "Queue deletion of an unattached volume", func(ctx context.Context, in *apiVolumeDeleteInput) (*apiAccepted, error) {
		v, err := e.clouds.VolumeAddress(in.Cloud, in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		return e.submitTask(ctx, in.IdempotencyKey, "volume.delete", compute.InfrastructureCommand{CloudID: in.Cloud, Node: v.Host, Path: v.Path})
	})
}

type apiKey struct {
	NamespaceID compute.NamespaceID `json:"namespace_id,omitempty"`
	Fingerprint string              `json:"fingerprint"`
	PublicKey   string              `json:"public_key"`
	Type        string              `json:"type"`
	Comment     string              `json:"comment"`
}

func keyDTO(k *compute.Key) apiKey {
	return apiKey{k.NamespaceID, k.Fingerprint, k.ValueString(), k.Type, k.Comment}
}

type apiKeyInput struct {
	Fingerprint string `path:"fingerprint"`
}
type apiKeyCreateInput struct {
	Body struct {
		PublicKey string `json:"public_key" minLength:"1" maxLength:"16384"`
	}
}

func (e *Environ) registerKeyOperations(api huma.API) {
	registerAPI(api, "GET", "/keys", "list-keys", "List SSH public keys", func(ctx context.Context, in *apiEmpty) (*apiResult[[]apiKey], error) {
		keys, scopeErr := e.keys.ForNamespace(requestNamespace(ctx))
		if scopeErr != nil {
			return nil, apiError(scopeErr)
		}
		ks, err := keys.List()
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiKey{}
		for _, k := range ks {
			out = append(out, keyDTO(k))
		}
		return &apiResult[[]apiKey]{Body: out}, nil
	})
	registerAPI(api, "GET", "/keys/{fingerprint}", "get-key", "Get SSH public key", func(ctx context.Context, in *apiKeyInput) (*apiResult[apiKey], error) {
		keys, scopeErr := e.keys.ForNamespace(requestNamespace(ctx))
		if scopeErr != nil {
			return nil, apiError(scopeErr)
		}
		k, err := keys.Get(in.Fingerprint)
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[apiKey]{Body: keyDTO(k)}, nil
	})
	registerAPI(api, "POST", "/keys", "add-key", "Add one SSH public key", func(ctx context.Context, in *apiKeyCreateInput) (*apiResult[apiKey], error) {
		keys, scopeErr := e.keys.ForNamespace(requestNamespace(ctx))
		if scopeErr != nil {
			return nil, apiError(scopeErr)
		}
		pub, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(in.Body.PublicKey))
		if err != nil || len(rest) != 0 {
			return nil, apiError(compute.ErrInvalid)
		}
		if err := keys.Add(in.Body.PublicKey); err != nil {
			return nil, apiError(err)
		}
		k, err := keys.Get(ssh.FingerprintLegacyMD5(pub))
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[apiKey]{Body: keyDTO(k)}, nil
	})
	registerAPI(api, "DELETE", "/keys/{fingerprint}", "delete-key", "Delete SSH public key", func(ctx context.Context, in *apiKeyInput) (*apiEmpty, error) {
		keys, scopeErr := e.keys.ForNamespace(requestNamespace(ctx))
		if scopeErr != nil {
			return nil, apiError(scopeErr)
		}
		if _, err := keys.Get(in.Fingerprint); err != nil {
			return nil, apiError(err)
		}
		return nil, apiError(keys.Delete(in.Fingerprint))
	})
}
