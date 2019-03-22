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
    Endpoints []ApiEndpoint `yaml:"endpoints"`
}

type ApiEndpoint struct {
    // TODO: Should this use the inheritable settings as well?
    Name string `yaml:"name"` // Required at all levels.
    Vars map[string]string `yaml:"vars,omitempty"`
    Paging map[string]string `yaml:"paging,omitempty"` // Optional
    Return string `yaml:"return,omitempty"` // Optional
    Endpoint string `yaml:"endpoint"` // Required
    CurrentBaseKey []string `yaml:"current_base_key,omitempty"` // Managing APIs that return a dict => list
    DesiredBaseKey []string `yaml:"desired_base_key,omitempty"` // Managing APIs that return a dict => list
    CurrentErrorKey []string `yaml:"current_error_key,omitempty"` // Managing APIs that return a dict => list
    DesiredErrorKey []string `yaml:"desired_error_key,omitempty"` // Managing APIs that return a dict => list
    Documentation string `yaml:"documentation,omitempty"` // Optional
    Params ApiParams `yaml:"params,flow,omitempty"` // Optional
    Endpoints map[string][]ApiEndpoint `yaml:"endpoints,omitempty"` // Iterating Key => Endpoint
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
    Uuid string
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
        Uuid: "",
        Endpoint: a.Endpoint,
        AttemptTime: a.AttemptTime,
        Time: a.Time,
        }
}


func (a ApiEndpoint) Copy() ApiEndpoint {
    var returnApiEndpoint ApiEndpoint

    returnApiEndpoint.Name = a.Name
    returnApiEndpoint.Vars = make(map[string]string)
    returnApiEndpoint.Paging = make(map[string]string)
    for k, v := range a.Vars {
        returnApiEndpoint.Vars[k] = v
    }
    for k, v := range a.Paging {
        returnApiEndpoint.Paging[k] = v
    }
    returnApiEndpoint.Return = a.Return
    returnApiEndpoint.Endpoint = a.Endpoint
    for _, v := range a.CurrentBaseKey {
        returnApiEndpoint.CurrentBaseKey = append(
            returnApiEndpoint.CurrentBaseKey, v )
    }
    for _, v := range a.DesiredBaseKey {
        returnApiEndpoint.DesiredBaseKey = append(
            returnApiEndpoint.DesiredBaseKey, v )
    }
    for _, v := range a.CurrentErrorKey {
        returnApiEndpoint.CurrentErrorKey = append(
            returnApiEndpoint.CurrentErrorKey, v )
    }
    for _, v := range a.DesiredErrorKey {
        returnApiEndpoint.DesiredErrorKey = append(
            returnApiEndpoint.DesiredErrorKey, v )
    }
    returnApiEndpoint.Documentation = a.Documentation
    returnApiEndpoint.Params = a.Params.Copy()
    returnApiEndpoint.Endpoints = make(map[string][]ApiEndpoint)
    for k, v := range a.Endpoints {
        for _, sv := range v {
            returnApiEndpoint.Endpoints[k] = append(
                returnApiEndpoint.Endpoints[k], sv )
        }
    }

    return returnApiEndpoint
}


func (a ApiParams) Copy() ApiParams {
    var returnApiParams ApiParams

    returnApiParams.QueryString = make(map[string][]string)
    returnApiParams.Header = make(map[string][]string)
    returnApiParams.Body = make(map[string][]string)
    for k, v := range a.QueryString {
        for _, sv := range v {
            returnApiParams.QueryString[k] = append(
                returnApiParams.QueryString[k], sv )
        }
    }
    for k, v := range a.Header {
        for _, sv := range v {
            returnApiParams.Header[k] = append( returnApiParams.Header[k], sv )
        }
    }
    for k, v := range a.Body {
        for _, sv := range v {
            returnApiParams.Body[k] = append( returnApiParams.Body[k], sv )
        }
    }

    return returnApiParams
}
