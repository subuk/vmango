package compute

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const MaxCloudInitUserDataBytes = 256 << 10

// CloudInitTemplate is a reusable user-data document. Revision identifies the
// complete saved document, so a caller can apply exactly the version it viewed.
type CloudInitTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	UserData    string `json:"user_data"`
	Revision    string `json:"revision"`
}

type CloudInitTemplateInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	UserData    string `json:"user_data"`
}

type CloudInitTemplateRef struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

type CloudInitTemplateRepository interface {
	List() ([]CloudInitTemplate, error)
	Get(id string) (CloudInitTemplate, error)
	Create(template CloudInitTemplate) error
	Update(template CloudInitTemplate, expectedRevision string) error
	Delete(id, revision string) error
}

type CloudInitTemplateService struct{ repo CloudInitTemplateRepository }

func NewCloudInitTemplateService(repo CloudInitTemplateRepository) *CloudInitTemplateService {
	return &CloudInitTemplateService{repo: repo}
}

// ValidateCloudInitUserData validates transport constraints without interpreting
// cloud-init syntax. Empty data is valid for a machine without custom user-data.
func ValidateCloudInitUserData(data string) error {
	if !utf8.ValidString(data) || strings.ContainsRune(data, 0) {
		return invalid("user_data", "User-data must be UTF-8 text without null characters.")
	}
	if len(data) > MaxCloudInitUserDataBytes {
		return invalid("user_data", "User-data must not exceed 256 KiB.")
	}
	return nil
}

func validCloudInitTemplateID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil && parsed.String() == id
}

func validateCloudInitTemplateRef(id, revision string) error {
	if !validCloudInitTemplateID(id) {
		return invalid("id", "Select a valid cloud-init template.")
	}
	if !validCloudInitTemplateID(revision) {
		return invalid("revision", "A valid template revision is required.")
	}
	return nil
}

func validateCloudInitTemplateInput(input CloudInitTemplateInput) error {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 128 || !utf8.ValidString(input.Name) || strings.ContainsAny(input.Name, "\x00\r\n") {
		return invalid("name", "Template name must be nonempty single-line text of at most 128 bytes.")
	}
	if len(input.Description) > 1024 || !utf8.ValidString(input.Description) || strings.ContainsRune(input.Description, 0) {
		return invalid("description", "Description must be valid text of at most 1024 bytes.")
	}
	if err := ValidateCloudInitUserData(input.UserData); err != nil {
		return err
	}
	if strings.TrimSpace(input.UserData) == "" {
		return invalid("user_data", "Template user-data must not be empty.")
	}
	return nil
}

func (s *CloudInitTemplateService) List() ([]CloudInitTemplate, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUnavailable
	}
	list, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []CloudInitTemplate{}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name == list[j].Name {
			return list[i].ID < list[j].ID
		}
		return list[i].Name < list[j].Name
	})
	return list, nil
}

func (s *CloudInitTemplateService) Get(id string) (CloudInitTemplate, error) {
	if !validCloudInitTemplateID(id) {
		return CloudInitTemplate{}, invalid("id", "Select a valid cloud-init template.")
	}
	if s == nil || s.repo == nil {
		return CloudInitTemplate{}, ErrUnavailable
	}
	return s.repo.Get(id)
}

func (s *CloudInitTemplateService) Create(input CloudInitTemplateInput) (CloudInitTemplate, error) {
	if err := validateCloudInitTemplateInput(input); err != nil {
		return CloudInitTemplate{}, err
	}
	if s == nil || s.repo == nil {
		return CloudInitTemplate{}, ErrUnavailable
	}
	template := CloudInitTemplate{ID: uuid.NewString(), Name: strings.TrimSpace(input.Name), Description: input.Description, UserData: input.UserData, Revision: uuid.NewString()}
	if err := s.repo.Create(template); err != nil {
		return CloudInitTemplate{}, err
	}
	return template, nil
}

func (s *CloudInitTemplateService) Update(id, revision string, input CloudInitTemplateInput) (CloudInitTemplate, error) {
	if err := validateCloudInitTemplateRef(id, revision); err != nil {
		return CloudInitTemplate{}, err
	}
	if err := validateCloudInitTemplateInput(input); err != nil {
		return CloudInitTemplate{}, err
	}
	if s == nil || s.repo == nil {
		return CloudInitTemplate{}, ErrUnavailable
	}
	template := CloudInitTemplate{ID: id, Name: strings.TrimSpace(input.Name), Description: input.Description, UserData: input.UserData, Revision: uuid.NewString()}
	if err := s.repo.Update(template, revision); err != nil {
		return CloudInitTemplate{}, err
	}
	return template, nil
}

func (s *CloudInitTemplateService) Delete(id, revision string) error {
	if err := validateCloudInitTemplateRef(id, revision); err != nil {
		return err
	}
	if s == nil || s.repo == nil {
		return ErrUnavailable
	}
	return s.repo.Delete(id, revision)
}

func (s *CloudInitTemplateService) Resolve(ref CloudInitTemplateRef) (string, error) {
	if err := validateCloudInitTemplateRef(ref.ID, ref.Revision); err != nil {
		return "", err
	}
	template, err := s.Get(ref.ID)
	if err != nil {
		return "", err
	}
	if template.Revision != ref.Revision {
		return "", fmt.Errorf("%w: cloud-init template changed; review the current version", ErrConflict)
	}
	return template.UserData, nil
}
