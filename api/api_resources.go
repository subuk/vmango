package api

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"golang.org/x/crypto/ssh"
	"subuk/vmango/compute"
)

type apiVolumeInput struct {
	Node   string `path:"node"`
	Volume string `path:"volume"`
}
type apiVolumeListInput struct {
	Node  string `path:"node"`
	Pool  string `query:"pool"`
	Usage string `query:"usage" enum:"attachable"`
}
type apiVolumeCreate struct {
	Name      string `json:"name" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$"`
	Pool      string `json:"pool" minLength:"1"`
	Format    string `json:"format,omitempty" enum:"raw,qcow2" default:"qcow2"`
	SizeBytes uint64 `json:"size_bytes" minimum:"1"`
}
type apiVolumeCreateInput struct {
	Node string `path:"node"`
	Body apiVolumeCreate
}
type apiVolumeCloneInput struct {
	Node   string `path:"node"`
	Volume string `path:"volume"`
	Body   apiVolumeCreate
}
type apiVolumeResizeInput struct {
	Node   string `path:"node"`
	Volume string `path:"volume"`
	Body   struct {
		SizeBytes uint64 `json:"size_bytes" minimum:"1"`
	}
}

func (e *Environ) registerVolumeOperations(api huma.API) {
	const path = "/nodes/{node}/volumes"
	registerAPI(api, "GET", path, "list-volumes", "List visible volumes", func(ctx context.Context, in *apiVolumeListInput) (*apiResult[[]apiVolume], error) {
		if _, err := e.nodes.Get(in.Node, compute.NodeGetOptions{NoPins: true}); err != nil {
			return nil, apiError(err)
		}
		opts := compute.VolumeListOptions{NodeIds: []string{in.Node}}
		if in.Pool != "" {
			opts.PoolNames = []string{in.Pool}
		}
		var vs []*compute.Volume
		var err error
		if in.Usage == "attachable" {
			vs, err = e.volumes.Attachable(opts, e.images)
		} else {
			vs, err = e.volumes.List(opts)
		}
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiVolume{}
		for _, v := range vs {
			if !v.Metadata.Hidden {
				out = append(out, volumeDTO(v))
			}
		}
		return &apiResult[[]apiVolume]{Body: out}, nil
	})
	registerAPI(api, "GET", path+"/{volume}", "get-volume", "Get volume", func(ctx context.Context, in *apiVolumeInput) (*apiResult[apiVolume], error) {
		p, err := volumePath(in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		v, err := e.volumes.Get(p, in.Node)
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[apiVolume]{Body: volumeDTO(v)}, nil
	})
	registerAPI(api, "POST", path, "create-volume", "Create volume", func(ctx context.Context, in *apiVolumeCreateInput) (*apiResult[apiVolume], error) {
		b := in.Body
		v, err := e.volumes.Create(compute.VolumeCreateParams{NodeId: in.Node, Name: b.Name, Pool: b.Pool, Format: compute.NewVolumeFormat(b.Format), Size: compute.NewSize(b.SizeBytes, compute.SizeUnitB)})
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[apiVolume]{Body: volumeDTO(v)}, nil
	})
	registerAPI(api, "POST", path+"/{volume}/clone", "clone-volume", "Clone volume", func(ctx context.Context, in *apiVolumeCloneInput) (*apiResult[apiVolume], error) {
		p, err := volumePath(in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		b := in.Body
		v, err := e.volumes.Clone(compute.VolumeCloneParams{NodeId: in.Node, OriginalPath: p, NewName: b.Name, NewPool: b.Pool, Format: compute.NewVolumeFormat(b.Format), NewSize: compute.NewSize(b.SizeBytes, compute.SizeUnitB)})
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[apiVolume]{Body: volumeDTO(v)}, nil
	})
	registerAPI(api, "POST", path+"/{volume}/resize", "resize-volume", "Grow a volume", func(ctx context.Context, in *apiVolumeResizeInput) (*apiEmpty, error) {
		p, err := volumePath(in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		return nil, apiError(e.volumes.Resize(p, in.Node, compute.NewSize(in.Body.SizeBytes, compute.SizeUnitB)))
	})
	registerAPI(api, "DELETE", path+"/{volume}", "delete-volume", "Delete an unprotected, unattached volume", func(ctx context.Context, in *apiVolumeInput) (*apiEmpty, error) {
		p, err := volumePath(in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		return nil, apiError(e.volumes.Delete(p, in.Node))
	})
}

type apiKey struct {
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
	Type        string `json:"type"`
	Comment     string `json:"comment"`
}

func keyDTO(k *compute.Key) apiKey { return apiKey{k.Fingerprint, k.ValueString(), k.Type, k.Comment} }

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
		ks, err := e.keys.List()
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
		k, err := e.keys.Get(in.Fingerprint)
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[apiKey]{Body: keyDTO(k)}, nil
	})
	registerAPI(api, "POST", "/keys", "add-key", "Add one SSH public key", func(ctx context.Context, in *apiKeyCreateInput) (*apiResult[apiKey], error) {
		pub, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(in.Body.PublicKey))
		if err != nil || len(rest) != 0 {
			return nil, apiError(compute.ErrInvalid)
		}
		if err := e.keys.Add(in.Body.PublicKey); err != nil {
			return nil, apiError(err)
		}
		k, err := e.keys.Get(ssh.FingerprintLegacyMD5(pub))
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[apiKey]{Body: keyDTO(k)}, nil
	})
	registerAPI(api, "DELETE", "/keys/{fingerprint}", "delete-key", "Delete SSH public key", func(ctx context.Context, in *apiKeyInput) (*apiEmpty, error) {
		if _, err := e.keys.Get(in.Fingerprint); err != nil {
			return nil, apiError(err)
		}
		return nil, apiError(e.keys.Delete(in.Fingerprint))
	})
}
