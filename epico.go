package epico

import (
    "encoding/json"
    "io/ioutil"
    "net/http"
    "plugin"
    "reflect"
    "strconv"
    "strings"
    "time"

    generic_structs "epico/structs"
    "epico/utils"

    "gopkg.in/yaml.v2"
    uuid "github.com/satori/go.uuid"
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
// TODO: Should this be passed as a JSON []byte/string we can just marshal?
func PullApiData( configLocation string, authParams []string, peekParams []string, postParams []string, additionalParams map[string]map[string]map[string]string ) []byte {

    api := generic_structs.ApiRoot{}

    responseList := make(map[generic_structs.ComparableApiRequest][]byte)
    var jsonKeys []map[string]string

    files, err := ioutil.ReadDir(configLocation)
    if err != nil {
        utils.LogFatal("PullApiData", "Unable to read config directory", err)
        return nil
    }

    // Declare this outside the process loop because the post process function
    //    gets applied to results of all API calls.
    var PluginPostProcessFunction = new( *func(
            map[generic_structs.ComparableApiRequest][]uint8,
            []map[string]string, []string)[]uint8 )

    for _, f := range files {
        rawYaml, err := ioutil.ReadFile(configLocation + f.Name())
        if err != nil {
            utils.LogFatal("PullApiData", "Error reading YAML API defnition",
                err)
        }

        err = yaml.Unmarshal([]byte(rawYaml), &api)
        if err != nil {
            utils.LogFatal("PullApiData",
                "Error unmarshaling YAML API definition", err)
        }

        // Do our YAML expansion so we can iterate through the various
        //    permutations.
        var expandedYamls [][]byte
        if len(api.VarsData) > 0 {
            expandedYamls = utils.PopulateYamlSlice( string(rawYaml),
                api.VarsData )
        } else {
            expandedYamls = append( expandedYamls, rawYaml )
        }

        for _, y := range expandedYamls {

            // Repull our data incase some expansion vars were in there.
            err = yaml.Unmarshal([]byte(y), &api)
            if err != nil {
                utils.LogFatal("PullApiData",
                    "Error unmarshaling YAML API definition", err)
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
                for i, v := range api.AuthParams {
                    if v == "{{}}" {
                        api.AuthParams[i] = authParams[cliCount]
                        cliCount += 1
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
                Name: api.Name,
                Vars: api.Vars,
                Paging: api.Paging,
                Plugin: api.Plugin,
                AuthParams: aps,
                PagingParams: paps,
            }


            // Load the plugin and functions for this config file.
            plug, err := plugin.Open(rootSettingsData.Plugin)
            if err != nil {
                utils.LogFatal("PullApiData", "Error opening plugin", err)
                return nil
            }

            var PluginAuthFunction = new( *func(generic_structs.ApiRequest,
                []string)generic_structs.ApiRequest )
            authSymbol, err := plug.Lookup("PluginAuthFunction")
            *PluginAuthFunction = authSymbol.( *func(generic_structs.ApiRequest,
                []string)generic_structs.ApiRequest)
            if err != nil {
                utils.LogFatal("PullApiData",
                    "Error looking up plugin Auth function", err)
            }

            var PluginResponseToJsonFunction = new( *func(map[string]string,
                []byte)[]byte )
            rtjSymbol, err := plug.Lookup("PluginResponseToJsonFunction")
            *PluginResponseToJsonFunction = rtjSymbol.( *func(map[string]string,
                []byte)[]byte)
            if err != nil {
                utils.LogFatal("PullApiData",
                    "Error looking up plugin ResponseToJson function", err)
            }

            // We only take the post processing from the first YAML we pull.
            if *PluginPostProcessFunction == nil {
                ppSymbol, err := plug.Lookup("PluginPostProcessFunction")
                *PluginPostProcessFunction = ppSymbol.( *func(
                    map[generic_structs.ComparableApiRequest][]uint8,
                    []map[string]string, []string)[]uint8 )
                if err != nil {
                    utils.LogFatal("PullApiData",
                        "Error looking up plugin PostProcess function", err)
                }
            }

            var PluginPagingPeekFunction = new( *func([]uint8, []string,
                interface {}, []string)(interface {}, bool) )
            paPSymbol, err := plug.Lookup("PluginPagingPeekFunction")
            *PluginPagingPeekFunction = paPSymbol.( *func([]uint8, []string,
                interface {}, []string) (interface {}, bool) )
            if err != nil {
                utils.LogFatal("PullApiData",
                    "Error looking up plugin PagingPeek function", err)
            }

            // TODO: This doesn't work with a sub endpoint that uses a different
            //     plugin.
            holderResponseList, holderJsonKeys := runThroughEndpoints( api.Endpoints, rootSettingsData, additionalParams, PluginAuthFunction, PluginResponseToJsonFunction, PluginPagingPeekFunction, true, 0 )
            for k, v := range holderResponseList {
                responseList[k] = v
            }
            jsonKeys = append( jsonKeys, holderJsonKeys ...)
        }
    }


    // Theoretically we could send each response to its own post-processing,
    //    but that kind of breaks the idea that we would return everything from
    //    a single external call as a single JSON blob.  So instead, we're just
    //    going to use the one provided in a general configuration file.
    var finalResponseValueList []reflect.Value
    finalResponseValueList = append( finalResponseValueList,
        reflect.ValueOf( responseList ), reflect.ValueOf( jsonKeys ),
        reflect.ValueOf( postParams ) )
    finalResponse := reflect.ValueOf( (**PluginPostProcessFunction) ).Call(
        finalResponseValueList )


    return finalResponse[0].Bytes()

}


func runThroughEndpoints( endpoints []generic_structs.ApiEndpoint, rootSettingsData generic_structs.ApiRequestInheritableSettings, additionalParams map[string]map[string]map[string]string, PluginAuthFunction **func(generic_structs.ApiRequest, []string)generic_structs.ApiRequest, PluginResponseToJsonFunction **func(map[string]string, []byte)[]byte, PluginPagingPeekFunction **func([]uint8, []string, interface {}, []string)(interface {}, bool), runSubEndpoints bool, depth int) (map[generic_structs.ComparableApiRequest][]byte, []map[string]string) {
    responseList := make(map[generic_structs.ComparableApiRequest][]byte)
    var jsonKeys []map[string]string

    for _, ep := range endpoints {
        // Clone and adjust settings map
        var name string
        var cbk, dbk, cek, dek []string
        var vars, paging map[string]string
        params := generic_structs.ApiParams{}

        // Pull substitution vars first so we can substitute while
        //    saving other variables
        if len(rootSettingsData.Vars) != 0 {
            vars = rootSettingsData.Vars
        } else {
            vars = make(map[string]string)
        }
        epSubs := false
        if len(ep.Vars) != 0 {
            epSubs = true
            for k, v := range ep.Vars {
                vars[k] = v
            }
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
            cbk = ep.CurrentBaseKey
        } else {
            cbk = []string(nil)
        }
        if len(ep.DesiredBaseKey) > 0 {
            dbk = ep.DesiredBaseKey
        } else {
            dbk = []string(nil)
        }
        if len(ep.CurrentErrorKey) > 0 {
            cek = ep.CurrentErrorKey
        } else {
            cek = []string(nil)
        }
        if len(ep.DesiredErrorKey) > 0 {
            dek = ep.DesiredErrorKey
        } else {
            dek = []string(nil)
        }
        if len(ep.Params.QueryString) != 0 ||
              len(ep.Params.Body) != 0 ||
              len(ep.Params.Header) != 0 {
            params = ep.Params
        } else {
            params = generic_structs.ApiParams{
                QueryString: make(map[string][]string),
                Header: make(map[string][]string),
                Body: make(map[string][]string),
            }
        }

        // Merge runtime params.
        for t, m := range additionalParams[ep.Name] {
            if t == "header" {
                for k, v := range m {
                    params.Header[k] = append( params.Header[k], v )
                }
            } else if t == "querystring" {
                for k, v := range m {
                    params.QueryString[k] = append(
                        params.QueryString[k], v )
                }
            } else if t == "body" {
                // TODO
            }
        }

        // If we have substitution vars, do the substitutions.
        if epSubs {
            for k, v := range ep.Vars {
                if len(cbk) != len(dbk) || len(cbk) != len(cek) ||
                      len(cbk) != len(dek) {
                    utils.LogFatal( "PullApiData",
                        "Current and desired key lists must be the same length.", nil )
                } else {
                    name = strings.Replace( name, "{{" + k + "}}", v,
                        -1 )
                    for i, _ := range cbk {
                        cbk[i] = strings.Replace( cbk[i],
                            "{{" + k + "}}", v, -1 )
                        dbk[i] = strings.Replace( dbk[i],
                            "{{" + k + "}}", v, -1 )
                        cek[i] = strings.Replace( cek[i],
                            "{{" + k + "}}", v, -1 )
                        dek[i] = strings.Replace( dek[i],
                            "{{" + k + "}}", v, -1 )
                    }
                    for pk, pv := range params.Header {
                        for li, item := range pv {
                            params.Header[pk][li] =
                                strings.Replace( item,
                                "{{" + k + "}}", v, -1 )
                        }
                    }
                    for pk, pv := range params.QueryString {
                        for li, item := range pv {
                            params.QueryString[pk][li] =
                                strings.Replace(
                                item, "{{" + k + "}}", v, -1 )
                        }
                    }
                    for pk, pv := range params.Body {
                        for li, item := range pv {
                            params.Body[pk][li] = strings.Replace(
                                item, "{{" + k + "}}", v, -1 )
                        }
                    }
                    ep.Endpoint = strings.Replace( ep.Endpoint,
                        "{{" + k + "}}", v, -1 )
                    ep.Documentation = strings.Replace(
                        ep.Documentation, "{{" + k + "}}", v, -1 )
                }
            }
        }

        tempRequest, err := http.NewRequest("GET", ep.Endpoint, nil)
        if err != nil {
            utils.LogFatal("PullApiData", "Error creating API request object",
                err)
        }

        // Create the endpoint key set for iterating on later in the
        //    post process.
        newUuid, err := uuid.NewV4()
        if err != nil {
            utils.LogFatal("PullApiData", "Unable to generate new UUID", err)
        }
        newKeySet := map[string]string{
                 "api_call_name": ep.Name,
                 "api_call_uuid": newUuid.String(),
            }
        // Add our endpoint vars here so we can access them later in the
        //    post process.
        for k, v := range ep.Vars {
            newKeySet[k] = v
        }
        // Allowing for multiple base keys and error keys breaks request
        //    comparability, so we need to add them to our extra keyset
        //    instead for usage later.
        newKeySet["key_count"] = strconv.Itoa(len(cbk))
        for i, _ := range cbk {
            newKeySet["current_base_key_" + strconv.Itoa(i)] = cbk[i]
            newKeySet["desired_base_key_" + strconv.Itoa(i)] = dbk[i]
            newKeySet["current_error_key_" + strconv.Itoa(i)] = cek[i]
            newKeySet["desired_error_key_" + strconv.Itoa(i)] = dek[i]
        }

        // TODO: This seems dreadfully inefficient...
        // Only add a new keyset if one like it doesn't exist
        found := false
        for _, v := range jsonKeys {
            if reflect.DeepEqual( v, newKeySet ) {
                found = true
            }
        }
        if !found {
            jsonKeys = append( jsonKeys, newKeySet )
        }

        // Create our new ApiRequest object with the extrapolated data
        newApiRequest := generic_structs.ApiRequest{
            Settings: generic_structs.ApiRequestInheritableSettings{
                Name: name,
                // Expandable vars are defined at the root only, and
                //    pulled from cach file then combined with static
                //    vars from EP.
                Vars: vars,
                Paging: paging,
            },
            Endpoint: ep.Endpoint,
            CurrentBaseKey: cbk,
            DesiredBaseKey: dbk,
            CurrentErrorKey: cek,
            DesiredErrorKey: dek,
            Params: params,
            FullRequest: tempRequest,
        }

        // Apply our passed vars to the header/qs/body.
        q := newApiRequest.FullRequest.URL.Query()
        h := newApiRequest.FullRequest.Header
        for k, v := range newApiRequest.Params.Header {
            if len(v) > 0 {
                h.Add(k, v[0]) // TODO: Handle multiple passed here in
            }                  //    the event we want to allow multiple
        }                      //    calls to the endpoint with diff
        for k, v := range newApiRequest.Params.QueryString { // params.
            if len(v) > 0 {
                q.Add(k, v[0]) // TODO: Same.
            }
        }

        // Create the first request here and capture the first response.
        // From there we will see if there are more before adding more.
        newApiRequest.FullRequest.URL.RawQuery = q.Encode()

        var requestValue []reflect.Value
        newApiRequest.Time = time.Now()
        requestValue = append( requestValue,
            reflect.ValueOf( newApiRequest ),
            reflect.ValueOf( rootSettingsData.AuthParams ) )
        finalRequest := reflect.ValueOf((**PluginAuthFunction)).Call(
            requestValue )
        response, responseHeaders := runApiRequest(
            finalRequest[0].Interface().(generic_structs.ApiRequest) )
        comRequest := newApiRequest.ToComparableApiRequest()
        comRequest.Uuid = newUuid.String()
        // If we've done a request to this endpoint before, append the
        //    result - otherwise, create a new key in our response Map.
        // Also, don't append the result if we don't want to return this data
        if ep.Return != "false" {
            if _, ok := responseList[comRequest]; ok {
                responseList[comRequest] = append(
                    responseList[comRequest], response... )
            } else {
                responseList[comRequest] = append(
                    make([]byte, 0), response... )
            }
        }
        // Add the first response to our new response list (map).  Now
        //    check if we need to page.

        // Here we handle multipart keys - response.key.key1 etc.
        var responseKeys []string
        if newApiRequest.Settings.Paging["indicator_from_structure"] ==
              "calculated" {
            // If this is a calculated paging var, then it should be a
            //    list with the results per page first and total
            //    results second. Since the multipart keys could be of
            //    different lengths, we store where the split is to
            //    break it up in the peek func.
            separateKeys := strings.Split(
                newApiRequest.Settings.Paging["indicator_from_field"],
                ",")
            if len(separateKeys) != 3 {
                utils.LogFatal("PullApiData",
                    "Calculated paging requires three values in a csv - current page number, results per page, total results.", nil)
            }
            responseKeys = []string{strconv.Itoa(len(
                strings.Split(separateKeys[0], "."))) +
                "," + strconv.Itoa(len(
                strings.Split( separateKeys[1], "." )))}
            for _, v := range separateKeys {
                responseKeys = append( responseKeys,
                    strings.Split( v, ".")... )
            }
        } else {
            responseKeys = strings.Split(
                newApiRequest.Settings.Paging["indicator_from_field"],
                ".")
        }

        // Call our peek function to see if we have a paging value.
        var pagingData reflect.Value
        if newApiRequest.Settings.Paging["location_from"] == "header" {
            pagingData = reflect.ValueOf( responseHeaders )
        } else { // Default: response body.
            pagingData = reflect.ValueOf( response )
        }
        var finalPeekValueList []reflect.Value
        finalPeekValueList = append(
            finalPeekValueList, pagingData, reflect.ValueOf(
            responseKeys ), reflect.ValueOf( (*interface{})(nil) ),
            reflect.ValueOf( rootSettingsData.PagingParams ) )
        peekValue := reflect.ValueOf(
            (**PluginPagingPeekFunction) ).Call( finalPeekValueList )
        pageValue := peekValue[0].Interface()
        morePages := peekValue[1].Bool()

        for morePages {
            oldPageValue := pageValue
            nextApiRequest := newApiRequest
            // Handle passing the paging indicator.
            // TODO: Handle "body"
            if nextApiRequest.Settings.Paging["location_to"] ==
                  "querystring" {
                // TODO: Change to 'case'
                if nextApiRequest.Settings.Paging[
                      "indicator_from_structure"] == "full_url" {
                    nextApiRequest.FullRequest.URL, err =
                        nextApiRequest.FullRequest.URL.Parse(
                        oldPageValue.(string) )
                    if err != nil {
                        utils.LogFatal("PullApiData", "Error parsing paging URL returned", err)
                    }
                } else if nextApiRequest.Settings.Paging[
                      "indicator_from_structure"] == "calculated" {
                    q := nextApiRequest.FullRequest.URL.Query()
                    q.Set( nextApiRequest.Settings.Paging[
                        "indicator_to_field"],
                        strconv.FormatFloat( oldPageValue.(float64),
                        'f', -1, 64 ) )
                    nextApiRequest.FullRequest.URL.RawQuery = q.Encode()
                } else {
                    // By default they just give us a param back.
                    q := nextApiRequest.FullRequest.URL.Query()
                    q.Set(nextApiRequest.Settings.Paging[
                        "indicator_to_field"], oldPageValue.(string))
                    nextApiRequest.FullRequest.URL.RawQuery = q.Encode()
                }

            } // TODO: Handle more options here then just QS?

            var newRequestValue []reflect.Value
            nextApiRequest.Time = time.Now()
            newRequestValue = append( newRequestValue,
                reflect.ValueOf( nextApiRequest ),
                reflect.ValueOf( rootSettingsData.AuthParams ) )
            newFinalRequest := reflect.ValueOf(
                (**PluginAuthFunction) ).Call( newRequestValue )
            newResponse, newResponseHeaders := runApiRequest(
                newFinalRequest[0].Interface().(generic_structs.ApiRequest) )

            comRequest = nextApiRequest.ToComparableApiRequest()
            comRequest.Uuid = newUuid.String()
            if ep.Return != "false" {
                if _, ok := responseList[comRequest]; ok {
                    responseList[comRequest] = append(
                        responseList[comRequest], newResponse... )
                } else {
                    responseList[comRequest] = append(
                        make([]byte, 0), newResponse... )
                }
            }

            var newResponseKeys []string
            if nextApiRequest.Settings.Paging["indicator_from_structure"] ==
                  "calculated" {
                // See above.
                separateKeys := strings.Split(
                    nextApiRequest.Settings.Paging["indicator_from_field"],
                    ",")
                if len(separateKeys) != 3 {
                    utils.LogFatal("PullApiData",
                        "Calculated paging requires three values in a csv - current page number, results per page, total results.", nil)
                }

                newResponseKeys = []string{strconv.Itoa(len(
                    strings.Split(separateKeys[0], "."))) +
                    "," + strconv.Itoa(len(
                    strings.Split( separateKeys[1], "." )))}

                for _, v := range separateKeys {
                    newResponseKeys = append( newResponseKeys,
                        strings.Split( v, ".")... )
                }
            } else {
                newResponseKeys = strings.Split(
                    nextApiRequest.Settings.Paging["indicator_from_field"],
                    "." )
            }

            // Call our peek function to see if we have a paging value.
            var pagingData reflect.Value
            if newApiRequest.Settings.Paging["location_from"] == "header" {
                pagingData = reflect.ValueOf( newResponseHeaders )
            } else { // Default: response body.
                pagingData = reflect.ValueOf( newResponse )
            }

            var finalPeekValueList []reflect.Value
            finalPeekValueList = append(
                finalPeekValueList, pagingData,
                reflect.ValueOf( newResponseKeys ),
                reflect.ValueOf( oldPageValue ),
                reflect.ValueOf( rootSettingsData.PagingParams ) )
            peekValue := reflect.ValueOf(
                (**PluginPagingPeekFunction) ).Call(
                finalPeekValueList )
            pageValue = peekValue[0].Interface()
            morePages = peekValue[1].Bool()

        }

        // How do we expand variables into sub endpoints (e.g. main
        //     endpoint is for us-east-1 but sub endpoint should do all)
        // TODO: Example: For now, if the instance is in us-east-1, the subcalls
        //     would be to.  Leaving for now.
        for key, sEp := range ep.Endpoints {
            // for matching keys in ep.Endpoint response
            //     create new endpoint epHolder
            //     expand endpoint_key into epHolder properties
            //     run calls on subendpoint
            var jsonConversionValue []reflect.Value
            jsonConversionValue = append( jsonConversionValue,
                reflect.ValueOf( ep.Vars ),
                reflect.ValueOf( response ) )
            finalJsonResponse := reflect.ValueOf( (
                **PluginResponseToJsonFunction ) ).Call( jsonConversionValue )

            pagingData := finalJsonResponse[0].Bytes()

            responseKeys = strings.Split( key, "." )
            var unparsedStructure map[string]interface{}
            err = json.Unmarshal(pagingData, &unparsedStructure)
            if err != nil {
                utils.LogFatal("runThroughEndpoints:SubEndpoints", "Error unmarshaling JSON", err)
            }

            keyValues := utils.ParseJsonSubStructure( responseKeys, 0, unparsedStructure )

            var epHolder []generic_structs.ApiEndpoint
            for _, endpoint := range sEp {
                // For each ID key returned, create a new endpoint and append
                for _, value := range keyValues {
                    var newSubEp generic_structs.ApiEndpoint
                    newSubEp = endpoint.Copy()
                    newSubEp.Vars["endpoint_key"] = value.(string)
                    epHolder = append( epHolder, newSubEp )
                }
            }

            // Recursively call this method for each sub endpoint.
            subResponseList, subJsonKeys := runThroughEndpoints( epHolder, rootSettingsData, additionalParams, PluginAuthFunction, PluginResponseToJsonFunction, PluginPagingPeekFunction, false, depth+1 )
            for k, v := range subResponseList {
                responseList[k] = v;
            }
            jsonKeys = append( jsonKeys, subJsonKeys ... )
        }

    }

    return responseList, jsonKeys
}


func runApiRequest( apiRequest generic_structs.ApiRequest ) ([]byte, []byte) {

    var client *http.Client
    if apiRequest.Client == nil {
        client = &http.Client{}
    } else {
        client = apiRequest.Client
    }
    resp, err := client.Do( apiRequest.FullRequest )
    if err != nil {
        utils.LogFatal("runApiRequest", "Error running the request", err)
    }
    defer resp.Body.Close()
    // TODO: Handle failed connections better / handle retry? Golang "Context"?
    // i/o timeoutpanic: runtime error: invalid memory address or nil pointer dereference
    // [signal SIGSEGV: segmentation violation code=0x1 addr=0x40 pc=0x6aa2ba]

    body, err := ioutil.ReadAll( resp.Body )
    if err != nil {
        utils.LogFatal("runApiRequest", "Error reading request body", err)
    }
    headers, err := json.Marshal( resp.Header )
    if err != nil {
        utils.LogFatal("runApiRequest", "Error reading request headers", err)
    }

    //for k, v := range apiRequest.FullRequest.Header {
    //    for _, rv := range v {
    //        utils.LogWarn("Request Headers", k + ": " + rv, nil)
    //    }
    //}
    //utils.LogWarn("Request", string(apiRequest.FullRequest.URL.String())+"\n\n", nil)
    //utils.LogWarn("Response Headers", string(headers)+"\n\n", nil)
    //utils.LogWarn("Response", string(body)+"\n\n", nil)

    return body, headers

}
