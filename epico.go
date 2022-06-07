package epico

import (
	"encoding/json"
	"fmt"
	"github.com/SREnity/epico/dashboard_reporter"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"plugin"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	uuid "github.com/satori/go.uuid"
	"gopkg.in/yaml.v2"

	generic_structs "github.com/SREnity/epico/structs"
	"github.com/SREnity/epico/utils"
)

// The meat of Epico and the only thing called externally - it handles parsing
//    YAML configs as well as calling plugin functions for auth/connection,
//    post-processing, and paging.  It returns a []byte of the condensed JSON
//    response from all configs/endpoints.
// Args:
// configLocation   = The folder where config YAMLs can be found for the plugin
//                    that is being used.
// authParams       = Plugin-specific auth parameters passed to the plugin being
//                    used.
// peekParams       = Plugin-specific peek parameters passed to the plugin being
//                    used.
// postParams       = Plugin-specific post parameters passed to the plugin being
//                    used.
// additionalParams = API-specific parameters for body, header, or querystring.
//                    Structure:
//                        {
//                            "ENDPOINT_NAME": {
//                                "header": {
//                                    "KEY1": "VALUE1"
//                                    ...
//                                },
//                                "querystring": {
//                                    "KEY1": "VALUE1"
//                                    ...
//                                },
//                                "body": {
//                                    "KEY1": "VALUE1"
//                                    ...
//                                },
//                            },
//                            ...
//                        }
// connectionOnly  = Parameter that if true, endpoints in yaml with use_for_connection_check set
//                   to true will be used, others skipped
// TODO: Should this be passed as a JSON []byte/string we can just marshal?
func PullApiData(configLocation string, authParams []string, peekParams []string, postParams []string, additionalParams map[string]map[string]map[string]string, connectionOnly bool, apiKey string, apiSecret string, pluginID int) []byte {
	api := generic_structs.ApiRoot{}

	responseList := make(map[generic_structs.ComparableApiRequest][]byte)
	var jsonKeys []map[string]string

	files, err := ioutil.ReadDir(configLocation)
	if err != nil {
		utils.LogError("PullApiData", "Unable to read config directory", err)
		return []byte(nil)
	}

	reporter := dashboard_reporter.Reporter{APIKey: apiKey, APISecret: apiSecret}
	if !connectionOnly {
		var scanLogs []dashboard_reporter.ScanLog
		scanLog := dashboard_reporter.ScanLog{
			Log_type: "plugin",
			Text:     "Accessing APIs:",
		}
		scanLogs = append(scanLogs, scanLog)

		err := reporter.AddScanLogs(pluginID, scanLogs)
		if err != nil {
			log.Printf("Error while updating plugin status: %s", err.Error())
		}
	}

	// Declare this outside the process loop because the post process function  gets applied to results of all API calls.
	var PluginPostProcessFunction = new(*func(map[generic_structs.ComparableApiRequest][]uint8, []map[string]string, []string) []uint8)

	for _, f := range files {
		rawYaml, err := ioutil.ReadFile(configLocation + f.Name())
		if err != nil {
			utils.LogError("PullApiData", "Error reading YAML API defnition", err)
			return []byte(nil)
		}

		err = yaml.Unmarshal([]byte(rawYaml), &api)
		if err != nil {
			utils.LogError("PullApiData", "Error unmarshaling YAML API definition", err)
			return []byte(nil)
		}

		// Do our YAML expansion so we can iterate through the various permutations.
		var expandedYamls [][]byte
		if len(api.VarsData) > 0 {
			expandedYamls = utils.PopulateYamlSlice(string(rawYaml), api.VarsData)
		} else {
			expandedYamls = append(expandedYamls, rawYaml)
		}

		for _, y := range expandedYamls {
			// Repull our data incase some expansion vars were in there.
			err = yaml.Unmarshal([]byte(y), &api)
			if err != nil {
				utils.LogError("PullApiData", "Error unmarshaling YAML API definition", err)
				return []byte(nil)
			}
			// Handle Params merging - options are:
			// - overwrite config file with CLI vars
			// - input CLI params into config file params at designated places
			//   (so method/keys can be in the config and potentially sensative
			//   vars passed from CLI)
			// Note: post processing is done across ALL YAMLs, and thus must be
			//    independent of any particular API.  That parameter is just
			//    passed at runtime.
			var aps, paps []string

			if len(api.AuthParams) == 0 {
				aps = authParams
			} else if len(authParams) == 0 {
				aps = api.AuthParams
			} else {
				cliCount := 0
				for i := range api.AuthParams {
					for {
						if !strings.Contains(api.AuthParams[i], "{{}}") {
							break
						}

						api.AuthParams[i] = strings.Replace(api.AuthParams[i], "{{}}", authParams[cliCount], 1)
						cliCount++
					}
				}
				aps = api.AuthParams
			}

			if len(api.PagingParams) == 0 {
				paps = peekParams
			} else if len(peekParams) == 0 {
				paps = api.PagingParams
			} else {
				cliCount := 0
				for i, v := range api.PagingParams {
					if v == "{{}}" {
						api.PagingParams[i] = peekParams[cliCount]
						cliCount += 1
					}
				}
				paps = api.PagingParams
			}

			rootSettingsData := generic_structs.ApiRequestInheritableSettings{
				Name:            api.Name,
				Vars:            api.Vars,
				Paging:          api.Paging,
				Plugin:          api.Plugin,
				AuthParams:      aps,
				PagingParams:    paps,
				GlobalVars:      api.GlobalVars,
				SkipContentType: api.SkipContentType,
			}

			// Load the plugin and functions for this config file.
			plug, err := plugin.Open(rootSettingsData.Plugin)
			if err != nil {
				utils.LogError("PullApiData", "Error opening plugin", err)
				return []byte(nil)
			}

			var PluginAuthFunction = new(*func(generic_structs.ApiRequest,
				[]string) generic_structs.ApiRequest)
			authSymbol, err := plug.Lookup("PluginAuthFunction")
			*PluginAuthFunction = authSymbol.(*func(generic_structs.ApiRequest,
				[]string) generic_structs.ApiRequest)
			if err != nil {
				utils.LogError("PullApiData", "Error looking up plugin Auth function", err)
				return []byte(nil)
			}

			var PluginResponseToJsonFunction = new(*func(map[string]string, []byte) []byte)
			rtjSymbol, err := plug.Lookup("PluginResponseToJsonFunction")
			*PluginResponseToJsonFunction = rtjSymbol.(*func(map[string]string, []byte) []byte)
			if err != nil {
				utils.LogError("PullApiData", "Error looking up plugin ResponseToJson function", err)
				return []byte(nil)
			}

			// We only take the post processing from the first YAML we pull.
			if *PluginPostProcessFunction == nil {
				ppSymbol, err := plug.Lookup("PluginPostProcessFunction")
				*PluginPostProcessFunction = ppSymbol.(*func(map[generic_structs.ComparableApiRequest][]uint8, []map[string]string, []string) []uint8)
				if err != nil {
					utils.LogError("PullApiData", "Error looking up plugin PostProcess function", err)
					return []byte(nil)
				}
			}

			var PluginPagingPeekFunction = new(*func([]uint8, []string, interface{}, []string) (interface{}, bool))
			paPSymbol, err := plug.Lookup("PluginPagingPeekFunction")
			*PluginPagingPeekFunction = paPSymbol.(*func([]uint8, []string, interface{}, []string) (interface{}, bool))
			if err != nil {
				utils.LogError("PullApiData", "Error looking up plugin PagingPeek function", err)
				return []byte(nil)
			}

			// TODO: This doesn't work with a sub endpoint that uses a different plugin.
			holderResponseList, holderJsonKeys := runThroughEndpoints(api.Endpoints, rootSettingsData, additionalParams, PluginAuthFunction, PluginResponseToJsonFunction, PluginPagingPeekFunction, true, 0, connectionOnly, reporter, pluginID)
			for k, v := range holderResponseList {
				responseList[k] = v
			}
			jsonKeys = append(jsonKeys, holderJsonKeys...)
		}
	}

	checkResult := make(map[string]string)
	checkResult["Errors"] = "Invalid Credentials"
	errorJson, _ := json.Marshal(checkResult)
	if len(responseList) == 0 {
		return errorJson
	}

	if connectionOnly {
		respCodeSuccess := false
		finalListElement := make(map[generic_structs.ComparableApiRequest][]byte)

		for k := range responseList {
			if k.ResponseCode >= 200 && k.ResponseCode <= 299 {
				respCodeSuccess = true
				finalListElement[k] = responseList[k]
			}
		}
		if !respCodeSuccess {
			for k := range responseList {
				response := string(responseList[k])
				if response == "[]" || strings.Contains(response,"</html>") || len(response)==0{
					return errorJson
				}
				//responses neither xml nor json
				if !strings.Contains(response,"{") || !strings.Contains(response,"<"){
					checkResult["Errors"] = response
					errorJson, _ := json.Marshal(checkResult)
					return errorJson
				}else {
					finalListElement[k] = responseList[k]
					break
				}
			}
		}
		responseList = finalListElement
	}

	// Theoretically we could send each response to its own post-processing,
	//    but that kind of breaks the idea that we would return everything from
	//    a single external call as a single JSON blob.  So instead, we're just
	//    going to use the one provided in a general configuration file.
	var finalResponseValueList []reflect.Value
	finalResponseValueList = append(finalResponseValueList,
		reflect.ValueOf(responseList), reflect.ValueOf(jsonKeys),
		reflect.ValueOf(postParams))
	finalResponse := reflect.ValueOf(**PluginPostProcessFunction).Call(
		finalResponseValueList)

	return finalResponse[0].Bytes()
}

func runThroughEndpoints(endpoints []generic_structs.ApiEndpoint, rootSettingsData generic_structs.ApiRequestInheritableSettings, additionalParams map[string]map[string]map[string]string, PluginAuthFunction **func(generic_structs.ApiRequest, []string) generic_structs.ApiRequest, PluginResponseToJsonFunction **func(map[string]string, []byte) []byte, PluginPagingPeekFunction **func([]uint8, []string, interface{}, []string) (interface{}, bool), runSubEndpoints bool, depth int, connectionOnly bool, reporter dashboard_reporter.Reporter, pluginID int) (map[generic_structs.ComparableApiRequest][]byte, []map[string]string) {
	responseList := make(map[generic_structs.ComparableApiRequest][]byte)
	var jsonKeys []map[string]string

	for _, ep := range endpoints {
		// Clone and adjust settings map
		if connectionOnly {
			if !ep.UseForConnCheck {
				continue
			}
		} else {
			if ep.SkipForScans {
				log.Printf("Endpoint marked to skip %s", ep.Name)
				continue
			}
		}

		var name string
		var currentBaseKey, desiredBaseKey, currentErrorKey, desiredErrorKey []string
		var vars, paging map[string]string
		params := generic_structs.ApiParams{}

		// Pull substitution vars first so we can substitute while saving other variables
		if len(rootSettingsData.Vars) != 0 {
			vars = rootSettingsData.Vars
		} else {
			vars = make(map[string]string)
		}
		doEndpointSubs := false
		if len(ep.Vars) != 0 {
			doEndpointSubs = true
			for k, v := range ep.Vars {
				vars[k] = v
			}
		}
		if len(rootSettingsData.GlobalVars) > 0 {
			doEndpointSubs = true
			for k, v := range rootSettingsData.GlobalVars {
				vars[k] = v
			}
		}

		skipEnpoints := false
		for k, v := range vars {
			if len(ep.SkipEndpoint[k]) > 0 && utils.StringInSlice(v, ep.SkipEndpoint[k]) > -1 {
				skipEnpoints = true
			}
		}
		if skipEnpoints {
			continue
		}

		if ep.Name != "" {
			name = ep.Name
		} else {
			name = rootSettingsData.Name
		}
		if len(ep.Paging) != 0 {
			paging = ep.Paging
		} else {
			paging = rootSettingsData.Paging
		}
		if len(ep.CurrentBaseKey) > 0 {
			currentBaseKey = ep.CurrentBaseKey
		} else {
			currentBaseKey = []string(nil)
		}
		if len(ep.DesiredBaseKey) > 0 {
			desiredBaseKey = ep.DesiredBaseKey
		} else {
			desiredBaseKey = []string(nil)
		}
		if len(ep.CurrentErrorKey) > 0 {
			currentErrorKey = ep.CurrentErrorKey
		} else {
			currentErrorKey = []string(nil)
		}
		if len(ep.DesiredErrorKey) > 0 {
			desiredErrorKey = ep.DesiredErrorKey
		} else {
			desiredErrorKey = []string(nil)
		}
		if len(ep.Params.QueryString) != 0 || len(ep.Params.Body) != 0 || len(ep.Params.Header) != 0 {
			params = ep.Params
		} else {
			params = generic_structs.ApiParams{
				QueryString: make(map[string][]string),
				Header:      make(map[string][]string),
				Body:        make(map[string][]string),
			}
		}

		timeRegex, err := regexp.Compile("^{{time:(\\-?.+)}}$")
		if err != nil {
			utils.LogError("runThroughEndpoints", "Failed to parse time regex", err)
			return map[generic_structs.ComparableApiRequest][]byte{}, []map[string]string{}
		}

		for k, v := range ep.Params.QueryString {
			for index, value := range v {
				if !strings.Contains(value, "{{") {
					continue
				}

				if strings.Contains(value, "{{time:") {
					matches := timeRegex.FindStringSubmatch(value)
					if matches == nil || len(matches[1]) == 0 {
						utils.LogError("runThroughEndpoints", "Invalid param value", value)
						return map[generic_structs.ComparableApiRequest][]byte{}, []map[string]string{}
					}
					if matches[1] == "now" {
						ep.Params.QueryString[k][index] = strconv.Itoa(int(time.Now().Unix()))
						continue
					}

					duration, err := time.ParseDuration(matches[1])
					if err != nil {
						utils.LogError("runThroughEndpoints", fmt.Sprintf("Failed to parse duration %s", value), err)
						return map[generic_structs.ComparableApiRequest][]byte{}, []map[string]string{}
					}
					ep.Params.QueryString[k][index] = strconv.Itoa(int(time.Now().Add(duration).Unix()))
				}
			}
		}

		// Merge runtime params.
		for t, m := range additionalParams[ep.Name] {
			if t == "header" {
				for k, v := range m {
					params.Header[k] = append(params.Header[k], v)
				}
			} else if t == "querystring" {
				for k, v := range m {
					params.QueryString[k] = append(params.QueryString[k], v)
				}
			} else if t == "body" {
				// TODO
			}
		}

		// If we have substitution vars, do the substitutions.
		if doEndpointSubs {
			for k, v := range vars {
				if len(currentBaseKey) != len(desiredBaseKey) || len(currentErrorKey) != len(desiredErrorKey) {
					utils.LogError("runThroughEndpoints", "Current and desired key lists must be the same length")
					return map[generic_structs.ComparableApiRequest][]byte{}, []map[string]string{}
				} else {
					name = strings.Replace(name, "{{"+k+"}}", v, -1)
					for i := range currentBaseKey {
						currentBaseKey[i] = strings.Replace(currentBaseKey[i], "{{"+k+"}}", v, -1)
						desiredBaseKey[i] = strings.Replace(desiredBaseKey[i], "{{"+k+"}}", v, -1)
					}
					for i := range currentErrorKey {
						currentErrorKey[i] = strings.Replace(currentErrorKey[i], "{{"+k+"}}", v, -1)
						desiredErrorKey[i] = strings.Replace(desiredErrorKey[i], "{{"+k+"}}", v, -1)
					}
					for pk, pv := range params.Header {
						for li, item := range pv {
							params.Header[pk][li] = strings.Replace(item, "{{"+k+"}}", v, -1)
						}
					}
					for pk, pv := range params.QueryString {
						for li, item := range pv {
							params.QueryString[pk][li] = strings.Replace(item, "{{"+k+"}}", v, -1)
						}
					}
					for pk, pv := range params.Body {
						for li, item := range pv {
							params.Body[pk][li] = strings.Replace(item, "{{"+k+"}}", v, -1)
						}
					}
					ep.Endpoint = strings.Replace(ep.Endpoint, "{{"+k+"}}", v, -1)
					if len(additionalParams["*"]) > 0 && len(additionalParams["*"]["var_params"]) > 0 {
						for varKey, varValue := range additionalParams["*"]["var_params"] {
							if strings.ToLower(varKey) == strings.ToLower(k) {
								ep.Endpoint = strings.Replace(ep.Endpoint, strings.ToUpper(k), varValue, -1)
							}
						}
					}
					ep.Documentation = strings.Replace(ep.Documentation, "{{"+k+"}}", v, -1)
				}
			}
		}

		tempRequest, err := http.NewRequest("GET", ep.Endpoint, nil)
		if err != nil {
			utils.LogError("runThroughEndpoints", "Error creating API request object", err)
			return map[generic_structs.ComparableApiRequest][]byte{}, []map[string]string{}
		}

		// Create the endpoint key set for iterating on later in the post process.
		newUuid, err := uuid.NewV4()
		if err != nil {
			utils.LogError("runThroughEndpoints", "Unable to generate new UUID", err)
			return map[generic_structs.ComparableApiRequest][]byte{}, []map[string]string{}
		}
		newKeySet := map[string]string{
			"api_call_name": ep.Name,
			"api_call_uuid": newUuid.String(),
		}
		// Add our endpoint vars here so we can access them later in the post process.
		for k, v := range ep.Vars {
			newKeySet[k] = v
		}
		// Allowing for multiple base keys and error keys breaks request
		//    comparability, so we need to add them to our extra keyset
		//    instead for usage later.
		newKeySet["key_count"] = strconv.Itoa(len(currentBaseKey))
		for i := range currentBaseKey {
			newKeySet["current_base_key_"+strconv.Itoa(i)] = currentBaseKey[i]
			newKeySet["desired_base_key_"+strconv.Itoa(i)] = desiredBaseKey[i]
		}
		for i := range currentErrorKey {
			newKeySet["current_error_key_"+strconv.Itoa(i)] = currentErrorKey[i]
			newKeySet["desired_error_key_"+strconv.Itoa(i)] = desiredErrorKey[i]
		}

		// TODO: This seems dreadfully inefficient...
		// Only add a new keyset if one like it doesn't exist
		found := false
		for _, v := range jsonKeys {
			if reflect.DeepEqual(v, newKeySet) {
				found = true
			}
		}
		if !found {
			jsonKeys = append(jsonKeys, newKeySet)
		}

		// Create our new ApiRequest object with the extrapolated data
		newApiRequest := generic_structs.ApiRequest{
			Settings: generic_structs.ApiRequestInheritableSettings{
				Name: name,
				// Expandable vars are defined at the root only, and pulled from cach file then combined with static vars from EP.
				Vars:            vars,
				Paging:          paging,
				SkipContentType: rootSettingsData.SkipContentType,
			},
			Endpoint:          ep.Endpoint,
			CurrentBaseKey:    currentBaseKey,
			DesiredBaseKey:    desiredBaseKey,
			CurrentErrorKey:   currentErrorKey,
			DesiredErrorKey:   desiredErrorKey,
			Params:            params,
			FullRequest:       tempRequest,
			EndpointKeyValues: ep.EndpointKeyValues,
		}

		// Apply our passed vars to the header/qs/body.
		q := newApiRequest.FullRequest.URL.Query()
		h := newApiRequest.FullRequest.Header
		for k, v := range newApiRequest.Params.Header {
			if len(v) > 0 {
				h.Add(k, v[0]) // TODO: Handle multiple passed here in
			} // the event we want to allow multiple
		} // calls to the endpoint with diff
		for k, v := range newApiRequest.Params.QueryString { // params.
			if len(v) > 1 {
				for _, val := range v {
					q.Add(k+"[]", val)
				}
			} else if len(v) == 1 {
				q.Add(k, v[0])
			}
		}

		if(!connectionOnly){
			var scanLogs []dashboard_reporter.ScanLog
			scanLog := dashboard_reporter.ScanLog{
				Log_type: "plugin",
				Additional_text_type: "info",
				Additional_text: "- " + humanize(ep.Name),
			}
			scanLogs = append(scanLogs, scanLog)

			err = reporter.AddScanLogs(pluginID, scanLogs)
			if err != nil {
				log.Printf("Error while updating plugin status: %s", err.Error())
			}
		}

		// Create the first request here and capture the first response.
		// From there we will see if there are more before adding more.
		newApiRequest.FullRequest.URL.RawQuery = q.Encode()

		var requestValue []reflect.Value
		newApiRequest.Time = time.Now()
		requestValue = append(requestValue, reflect.ValueOf(newApiRequest), reflect.ValueOf(rootSettingsData.AuthParams))
		finalRequest := reflect.ValueOf(**PluginAuthFunction).Call(requestValue)
		statusCode, response, responseHeaders := runApiRequest(finalRequest[0].Interface().(generic_structs.ApiRequest))
		if statusCode < 200 || statusCode > 299 {
			utils.LogWarning("runThroughEndpoints", "[" + ep.Name + "]", fmt.Sprintf("Expected response status 2xx, got %d", statusCode))
			if !connectionOnly {
				continue
			}
		}

		comRequest := newApiRequest.ToComparableApiRequest()
		comRequest.Uuid = newUuid.String()
		if connectionOnly {
			comRequest.ResponseCode = statusCode
		}
		// If we've done a request to this endpoint before, append the
		//    result - otherwise, create a new key in our response Map.
		// Also, don't append the result if we don't want to return this data
		if ep.Return != "false" {
			if _, ok := responseList[comRequest]; ok {
				responseList[comRequest] = append(responseList[comRequest], response...)
			} else {
				responseList[comRequest] = append(make([]byte, 0), response...)
			}
		}
		// Add the first response to our new response list (map). Now check if we need to page.

		// Here we handle multipart keys - response.key.key1 etc.
		var responseKeys []string
		if newApiRequest.Settings.Paging["indicator_from_structure"] ==
			"calculated" {
			// If this is a calculated paging var, then it should be a
			//    list with the results per page first and total
			//    results second. Since the multipart keys could be of
			//    different lengths, we store where the split is to
			//    break it up in the peek func.
			separateKeys := strings.Split(newApiRequest.Settings.Paging["indicator_from_field"], ",")
			if len(separateKeys) != 3 {
				utils.LogError("runThroughEndpoints", "Calculated paging requires three values - current page number, results per page, total results")
				return map[generic_structs.ComparableApiRequest][]byte{}, []map[string]string{}
			}
			responseKeys = []string{strconv.Itoa(len(strings.Split(separateKeys[0], "."))) + "," + strconv.Itoa(len(strings.Split(separateKeys[1], ".")))}
			for _, v := range separateKeys {
				responseKeys = append(responseKeys, strings.Split(v, ".")...)
			}
		} else {
			responseKeys = strings.Split(newApiRequest.Settings.Paging["indicator_from_field"], ".")
		}

		// Call our peek function to see if we have a paging value.
		var pagingData reflect.Value
		if newApiRequest.Settings.Paging["location_from"] == "header" {
			pagingData = reflect.ValueOf(responseHeaders)
		} else { // Default: response body.
			pagingData = reflect.ValueOf(response)
		}
		var finalPeekValueList []reflect.Value
		finalPeekValueList = append(finalPeekValueList, pagingData, reflect.ValueOf(responseKeys), reflect.ValueOf((*interface{})(nil)), reflect.ValueOf(rootSettingsData.PagingParams))
		peekValue := reflect.ValueOf(**PluginPagingPeekFunction).Call(finalPeekValueList)
		pageValue := peekValue[0].Interface()
		morePages := peekValue[1].Bool()

		for morePages {
			oldPageValue := pageValue
			nextApiRequest := newApiRequest
			// Handle passing the paging indicator.
			// TODO: Handle "body"
			if nextApiRequest.Settings.Paging["location_to"] == "querystring" {
				// TODO: Change to 'case'
				if nextApiRequest.Settings.Paging["indicator_from_structure"] == "full_url" {
					nextApiRequest.FullRequest.URL, err = nextApiRequest.FullRequest.URL.Parse(oldPageValue.(string))
					if err != nil {
						utils.LogError("runThroughEndpoints", "Error parsing paging URL returned", err)
						return map[generic_structs.ComparableApiRequest][]byte{}, []map[string]string{}
					}
				} else if nextApiRequest.Settings.Paging["indicator_from_structure"] == "calculated" {
					q := nextApiRequest.FullRequest.URL.Query()
					q.Set(nextApiRequest.Settings.Paging["indicator_to_field"], strconv.FormatFloat(oldPageValue.(float64), 'f', -1, 64))
					nextApiRequest.FullRequest.URL.RawQuery = q.Encode()
				} else {
					// By default they just give us a param back.
					q := nextApiRequest.FullRequest.URL.Query()
					q.Set(nextApiRequest.Settings.Paging["indicator_to_field"], oldPageValue.(string))
					nextApiRequest.FullRequest.URL.RawQuery = q.Encode()
				}

			} // TODO: Handle more options here then just QS?

			var newRequestValue []reflect.Value
			nextApiRequest.Time = time.Now()
			newRequestValue = append(newRequestValue, reflect.ValueOf(nextApiRequest), reflect.ValueOf(rootSettingsData.AuthParams))
			newFinalRequest := reflect.ValueOf(**PluginAuthFunction).Call(newRequestValue)
			newStatusCode, newResponse, newResponseHeaders := runApiRequest(newFinalRequest[0].Interface().(generic_structs.ApiRequest))
			if newStatusCode < 200 || newStatusCode > 299 {
				utils.LogWarning("runThroughEndpoints", "[" + ep.Name + "]", fmt.Sprintf("Expected new response status 2xx, got %d", newStatusCode))
			}

			comRequest = nextApiRequest.ToComparableApiRequest()
			comRequest.Uuid = newUuid.String()
			if ep.Return != "false" {
				if _, ok := responseList[comRequest]; ok {
					responseList[comRequest] = append(responseList[comRequest], newResponse...)
				} else {
					responseList[comRequest] = append(make([]byte, 0), newResponse...)
				}
			}

			var newResponseKeys []string
			if nextApiRequest.Settings.Paging["indicator_from_structure"] ==
				"calculated" {
				// See above.
				separateKeys := strings.Split(nextApiRequest.Settings.Paging["indicator_from_field"], ",")
				if len(separateKeys) != 3 {
					utils.LogError("runThroughEndpoints", "Calculated paging requires three values - current page number, results per page, total results")
					return map[generic_structs.ComparableApiRequest][]byte{}, []map[string]string{}
				}

				newResponseKeys = []string{strconv.Itoa(len(strings.Split(separateKeys[0], "."))) + "," + strconv.Itoa(len(strings.Split(separateKeys[1], ".")))}

				for _, v := range separateKeys {
					newResponseKeys = append(newResponseKeys, strings.Split(v, ".")...)
				}
			} else {
				newResponseKeys = strings.Split(nextApiRequest.Settings.Paging["indicator_from_field"], ".")
			}

			// Call our peek function to see if we have a paging value.
			var pagingData reflect.Value
			if newApiRequest.Settings.Paging["location_from"] == "header" {
				pagingData = reflect.ValueOf(newResponseHeaders)
			} else { // Default: response body.
				pagingData = reflect.ValueOf(newResponse)
			}

			var finalPeekValueList []reflect.Value
			finalPeekValueList = append(
				finalPeekValueList, pagingData,
				reflect.ValueOf(newResponseKeys),
				reflect.ValueOf(oldPageValue),
				reflect.ValueOf(rootSettingsData.PagingParams))
			peekValue := reflect.ValueOf(**PluginPagingPeekFunction).Call(finalPeekValueList)
			pageValue = peekValue[0].Interface()
			morePages = peekValue[1].Bool()
		}

		// How do we expand variables into sub endpoints (e.g. main endpoint is for us-east-1 but sub endpoint should do all)
		// TODO: Example: For now, if the instance is in us-east-1, the subcalls would be to.  Leaving for now.
		for key, sEp := range ep.Endpoints {
			// for matching keys in ep.Endpoint response
			//     create new endpoint epHolder
			//     expand endpoint_key into epHolder properties
			//     run calls on subendpoint
			var jsonConversionValue []reflect.Value
			jsonConversionValue = append(jsonConversionValue, reflect.ValueOf(ep.Vars), reflect.ValueOf(response))
			finalJsonResponse := reflect.ValueOf(**PluginResponseToJsonFunction).Call(jsonConversionValue)

			pagingData := finalJsonResponse[0].Bytes()

			responseKeys = strings.Split(key, ".")
			var unparsedArrayStructure []map[string]interface{}
			var unparsedStructure map[string]interface{}
			if err := json.Unmarshal(pagingData, &unparsedArrayStructure); err != nil {
				if err := json.Unmarshal(pagingData, &unparsedStructure); err != nil {
					utils.LogError("runThroughEndpoints:SubEndpoints", "Error unmarshaling JSON", err)
					return map[generic_structs.ComparableApiRequest][]byte{}, []map[string]string{}
				}
				unparsedArrayStructure = append(unparsedArrayStructure, unparsedStructure)
			}
			keyValues := utils.ParseJsonSubStructure(responseKeys, 0, unparsedArrayStructure)

			var epHolder []generic_structs.ApiEndpoint
			for _, endpoint := range sEp {
				// For each ID key returned, create a new endpoint and append
				for keyValueIndex, value := range keyValues {
					var newSubEp generic_structs.ApiEndpoint
					newSubEp = endpoint.Copy()
					var endpointKey string
					switch tp := value.(type) {
					case string:
						endpointKey = value.(string)
					case float64:
						endpointKey = strconv.FormatFloat(value.(float64), 'f', -1, 64)
					case float32:
						endpointKey = strconv.FormatFloat(float64(value.(float32)), 'f', -1, 32)
					case int:
						endpointKey = strconv.Itoa(value.(int))
					case int64:
						endpointKey = strconv.FormatInt(value.(int64), 10)
					default:
						utils.LogError("runThroughEndpoints:SubEndpoints:EndpointKey", fmt.Sprintf("Unrecognized value type: %#v", tp))
						return map[generic_structs.ComparableApiRequest][]byte{}, []map[string]string{}
					}
					newSubEp.EndpointKeyValues = make(map[string]interface{})
					for endpointSourceKeyName, endpointTargetKeyName := range endpoint.EndpointKeyNames {
						if endpointSourceKeyName == "{{endpoint_key}}" {
							newSubEp.EndpointKeyValues[endpointTargetKeyName] = endpointKey
							continue
						}

						// Not quite optimal, because it's being regenerated on every iteration in parent endpoint results
						subStructure := utils.ParseJsonSubStructure(strings.Split(endpointSourceKeyName, "."), 0, unparsedArrayStructure)
						if len(subStructure) > 0 {
							newSubEp.EndpointKeyValues[endpointTargetKeyName] = subStructure[keyValueIndex]
						} else if len(ep.EndpointKeyValues) > 0 { // Trying to take value from parent if any
							value, ok := ep.EndpointKeyValues[endpointTargetKeyName]
							if ok {
								newSubEp.EndpointKeyValues[endpointTargetKeyName] = value
							}
						}
					}

					for k, v := range newSubEp.EndpointKeyValues {
						if v == nil {
							continue
						}

						strValue, ok := v.(string)
						if !ok {
							continue
						}

						newSubEp.Endpoint = strings.Replace(newSubEp.Endpoint, "{{"+k+"}}", strValue, -1)
					}

					newSubEp.Vars["endpoint_key"] = endpointKey
					epHolder = append(epHolder, newSubEp)
				}
			}

			// Recursively call this method for each sub endpoint.
			subResponseList, subJsonKeys := runThroughEndpoints(epHolder, rootSettingsData, additionalParams, PluginAuthFunction, PluginResponseToJsonFunction, PluginPagingPeekFunction, false, depth+1, connectionOnly, reporter, pluginID )
			for k, v := range subResponseList {
				responseList[k] = v
			}
			jsonKeys = append(jsonKeys, subJsonKeys...)
		}
	}

	return responseList, jsonKeys
}

func runApiRequest(apiRequest generic_structs.ApiRequest) (int, []byte, []byte) {
	logRequest := os.Getenv("EPICO_LOG_REQUEST")
	if logRequest == "true" {
		utils.LogInfo("runApiRequest", "Request", apiRequest.FullRequest.Method + " " + apiRequest.FullRequest.URL.String())
	}

	logRequestHeaders := os.Getenv("EPICO_LOG_REQUEST_HEADERS")
	if logRequestHeaders == "true" {
		utils.LogInfo("runApiRequest", "Request Headers", apiRequest.FullRequest.Header)
	}

	logRequestBody := os.Getenv("EPICO_LOG_REQUEST_BODY")
	if logRequestBody == "true" {
		body, _ := ioutil.ReadAll(apiRequest.FullRequest.Body)
		utils.LogInfo("runApiRequest", "Request Body", string(body))
	}

	var client *http.Client
	if apiRequest.Client == nil {
		client = &http.Client{}
	} else {
		client = apiRequest.Client
	}

	resp, err := client.Do(apiRequest.FullRequest)
	if err != nil {
		utils.LogError("runApiRequest", "Error running the request", err)
		return 400, []byte("[]"), []byte("[]")
	}
	defer resp.Body.Close()
	// TODO: Handle failed connections better / handle retry? Golang "Context"?
	// i/o timeoutpanic: runtime error: invalid memory address or nil pointer dereference
	// [signal SIGSEGV: segmentation violation code=0x1 addr=0x40 pc=0x6aa2ba]

	headers, err := json.Marshal(resp.Header)
	if err != nil {
		utils.LogError("runApiRequest", "Error reading request headers", err)
		return resp.StatusCode, []byte("[]"), []byte("[]")
	}

	logResponseHeaders := os.Getenv("EPICO_LOG_RESPONSE_HEADERS")
	if logResponseHeaders == "true" {
		utils.LogInfo("runApiRequest", "Response Headers", resp.Header)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		utils.LogError("runApiRequest", "Error reading response body", err)
		return resp.StatusCode, []byte("[]"), []byte("[]")
	}
	if resp.StatusCode == 204 && len(body) == 0 {
		body = []byte("[]")
	}

	logResponse := os.Getenv("EPICO_LOG_RESPONSE")
	if logResponse == "true" {
		utils.LogInfo("runApiRequest", "Response", string(body))
	}

	return resp.StatusCode, body, headers
}

func humanize(value string) (humanized string){
		isToUpper := false
		for k, v := range value {
			if k == 0 {
				humanized = strings.ToUpper(string(value[0]))
			} else {
				if isToUpper{
					humanized += " " + strings.ToUpper(string(v))
					isToUpper = false
				} else {
					if (v == '_') || (v == ' ') {
						isToUpper = true
					} else {
						humanized += string(v)
					}
				}
			}
		}
		return
}
