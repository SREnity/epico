package structs

import (
    "time"
    "net/http"
)

type ApiRoot struct {
    Name string `yaml:"name"` // Required
    VarsData map[string][]string `yaml:"vars_data",omitempty`
    Vars map[string]string `yaml:"vars",omitempty`
    Paging map[string]string `yaml:"paging"` // Required
    Plugin string `yaml:"plugin"` // Required
    AuthParams []string `yaml:"auth_params"`
    PagingParams []string `yaml:"paging_params"`
    PostProcessParams []string `yaml:"post_process_params"`
    Endpoints []ApiEndpoint `yaml:"endpoints"`
}

type ApiEndpoint struct {
    // TODO: Should this use the inheritable settings as well?
    Name string `yaml:"name"` // Required at all levels.
    Vars map[string]string `yaml:"vars,omitempty"`
    Paging map[string]string `yaml:"paging,omitempty"` // Optional
    Endpoint string `yaml:"endpoint"` // Required
    CurrentBaseKey []string `yaml:"current_base_key,omitempty"` // Managing APIs that return a dict => list
    DesiredBaseKey []string `yaml:"desired_base_key,omitempty"` // Managing APIs that return a dict => list
    CurrentErrorKey []string `yaml:"current_error_key,omitempty"` // Managing APIs that return a dict => list
    DesiredErrorKey []string `yaml:"desired_error_key,omitempty"` // Managing APIs that return a dict => list
    Documentation string `yaml:"documentation,omitempty"` // Optional
    Params ApiParams `yaml:"params,flow,omitempty"` // Optional
    Endpoints map[string]ApiEndpoint `yaml:"endpoints,omitempty"` // Iterating Key => Endpoint
}

type ApiRequest struct {
    Settings ApiRequestInheritableSettings
    Endpoint string
    CurrentBaseKey []string // Managing APIs that return a dict => list
    DesiredBaseKey []string // Managing APIs that return a dict => list
    CurrentErrorKey []string
    DesiredErrorKey []string
    Params ApiParams

    FullRequest *http.Request
    Client *http.Client

    AttemptTime time.Time
    Time time.Time
}

type ComparableApiRequest struct {
    Name string
    Endpoint string
    AttemptTime time.Time
    Time time.Time
}

type ApiRequestInheritableSettings struct {
    Name string
    Vars map[string]string `yaml:"vars",omitempty`
    Paging map[string]string
    Plugin string `yaml:"plugin"` // Required
    AuthParams []string `yaml:"auth_params"`
    PagingParams []string `yaml:"paging_params"`
    PostProcessParams []string `yaml:"post_process_params"`
}

type ApiParams struct {
    QueryString map[string][]string `yaml:"querystring,flow,omitempty"`
    Header map[string][]string `yaml:"header,flow,omitempty"`
    Body map[string][]string `yaml:"body,flow,omitempty"`
}

type ApiCredentials struct {
    Id string
    Key string
    Token string
}

func (a ApiRequest) ToComparableApiRequest() ComparableApiRequest {
    return ComparableApiRequest{
        Name: a.Settings.Name,
        Endpoint: a.Endpoint,
        AttemptTime: a.AttemptTime,
        Time: a.Time,
        }
}
