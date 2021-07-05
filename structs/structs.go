package structs

import (
	"encoding/json"
	"net/http"
	"time"
)

type ApiRoot struct {
	Name            string              `yaml:"name"` // Required
	VarsData        map[string][]string `yaml:"vars_data",omitempty`
	Vars            map[string]string   `yaml:"vars",omitempty`
	Paging          map[string]string   `yaml:"paging"` // Required
	Plugin          string              `yaml:"plugin"` // Required
	AuthParams      []string            `yaml:"auth_params"`
	PagingParams    []string            `yaml:"paging_params"`
	Endpoints       []ApiEndpoint       `yaml:"endpoints"`
	GlobalVars      map[string]string   `yaml:"global_vars",omitempty`       // Needed for substitutions in all the endpoints
	SkipContentType bool                `yaml:"skip_content_type,omitempty"` // Needed for skipping setting Content-Type header to application/json
}

type ApiEndpoint struct {
	// TODO: Should this use the inheritable settings as well?
	Name              string              `yaml:"name"` // Required at all levels.
	Vars              map[string]string   `yaml:"vars,omitempty"`
	SkipEndpoint      map[string][]string `yaml:"skip_endpoint,omitempty"`            // Optional
	Paging            map[string]string   `yaml:"paging,omitempty"`                   // Optional
	Return            string              `yaml:"return,omitempty"`                   // Optional
	UseForConnCheck   bool                `yaml:"use_for_connection_check,omitempty"` // Optional
	SkipForScans      bool                `yaml:"skip_for_scans,omitempty"`           // Optional
	Endpoint          string              `yaml:"endpoint"`                           // Required
	CurrentBaseKey    []string            `yaml:"current_base_key,omitempty"`         // Managing APIs that return a dict => list
	DesiredBaseKey    []string            `yaml:"desired_base_key,omitempty"`         // Managing APIs that return a dict => list
	CurrentErrorKey   []string            `yaml:"current_error_key,omitempty"`        // Managing APIs that return a dict => list
	DesiredErrorKey   []string            `yaml:"desired_error_key,omitempty"`        // Managing APIs that return a dict => list
	EndpointKeyNames  map[string]string   `yaml:"endpoint_key_names,omitempty"`       // Needed for adding endpoint key to sub-endpoint JSON
	EndpointKeyValues map[string]interface{}
	Documentation     string                   `yaml:"documentation,omitempty"` // Optional
	Params            ApiParams                `yaml:"params,flow,omitempty"`   // Optional
	Endpoints         map[string][]ApiEndpoint `yaml:"endpoints,omitempty"`     // Iterating Key => Endpoint
}

type ApiRequest struct {
	Settings          ApiRequestInheritableSettings
	Endpoint          string
	CurrentBaseKey    []string // Managing APIs that return a dict => list
	DesiredBaseKey    []string // Managing APIs that return a dict => list
	CurrentErrorKey   []string
	DesiredErrorKey   []string
	EndpointKeyValues map[string]interface{}
	Params            ApiParams

	FullRequest *http.Request
	Client      *http.Client

	AttemptTime time.Time
	Time        time.Time
}

type ComparableApiRequest struct {
	Name              string
	Uuid              string
	Endpoint          string
	EndpointKeyValues string
	AttemptTime       time.Time
	Time              time.Time
	ResponseCode      int
}

type ApiRequestInheritableSettings struct {
	Name            string
	Vars            map[string]string `yaml:"vars",omitempty`
	Paging          map[string]string
	Plugin          string            `yaml:"plugin"` // Required
	AuthParams      []string          `yaml:"auth_params"`
	PagingParams    []string          `yaml:"paging_params"`
	GlobalVars      map[string]string `yaml:"global_vars,omitempty"`       // Needed for substitutions in all the endpoints
	SkipContentType bool              `yaml:"skip_content_type,omitempty"` // Skip setting content-type header to application/json
}

type ApiParams struct {
	QueryString map[string][]string `yaml:"querystring,flow,omitempty"`
	Header      map[string][]string `yaml:"header,flow,omitempty"`
	Body        map[string][]string `yaml:"body,flow,omitempty"`
}

type ApiCredentials struct {
	Id    string
	Key   string
	Token string
}

func (a ApiRequest) ToComparableApiRequest() ComparableApiRequest {
	serializedKeyValues := ""
	if len(a.EndpointKeyValues) > 0 {
		serializedKeyValuesBytes, err := json.Marshal(a.EndpointKeyValues)
		// Ignore serialization errors. That's on purpose, since JSON should be valid here
		// (comes from code)
		if err == nil {
			serializedKeyValues = string(serializedKeyValuesBytes)
		}
	}

	return ComparableApiRequest{
		Name:              a.Settings.Name,
		Uuid:              "",
		Endpoint:          a.Endpoint,
		AttemptTime:       a.AttemptTime,
		Time:              a.Time,
		EndpointKeyValues: serializedKeyValues,
		ResponseCode:      200,
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
			returnApiEndpoint.CurrentBaseKey, v)
	}
	for _, v := range a.DesiredBaseKey {
		returnApiEndpoint.DesiredBaseKey = append(
			returnApiEndpoint.DesiredBaseKey, v)
	}
	for _, v := range a.CurrentErrorKey {
		returnApiEndpoint.CurrentErrorKey = append(
			returnApiEndpoint.CurrentErrorKey, v)
	}
	for _, v := range a.DesiredErrorKey {
		returnApiEndpoint.DesiredErrorKey = append(
			returnApiEndpoint.DesiredErrorKey, v)
	}
	returnApiEndpoint.Documentation = a.Documentation
	returnApiEndpoint.Params = a.Params.Copy()
	returnApiEndpoint.Endpoints = make(map[string][]ApiEndpoint)
	for k, v := range a.Endpoints {
		for _, sv := range v {
			returnApiEndpoint.Endpoints[k] = append(
				returnApiEndpoint.Endpoints[k], sv)
		}
	}
	returnApiEndpoint.EndpointKeyNames = make(map[string]string)
	for k, v := range a.EndpointKeyNames {
		returnApiEndpoint.EndpointKeyNames[k] = v
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
				returnApiParams.QueryString[k], sv)
		}
	}
	for k, v := range a.Header {
		for _, sv := range v {
			returnApiParams.Header[k] = append(returnApiParams.Header[k], sv)
		}
	}
	for k, v := range a.Body {
		for _, sv := range v {
			returnApiParams.Body[k] = append(returnApiParams.Body[k], sv)
		}
	}

	return returnApiParams
}
