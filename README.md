About
---
Epico aims to make API ingestion easy.  By providing a common framework to manage API calls and plugins to handle any API-specific logic - Epico aims to make API ingestion to be as simple as creating a YAML representing the endpoints you want to ingest. 

Example Usage
---
```
package main

import (
    "fmt"
    epico "./epico"
)

func main() {
    // Arg 1: This is the path to your epico YAML configs directory.
    // Arg 2: This is the path to your desired epico plugin SO file.
    // Arg 3: This is the authentication creds and any other plugin-specific
    // Arg 4: This is the peek function plugin-specific configuration variables
    //        required.
    // Arg 5: This is the post process function plugin-specific configuration
    //        variables required.
    // Arg 6: This allows for the passing of header, querystring, and body
    //        parameters at runtime.
    //            Structure:
    //                {
    //                    "ENDPOINT_NAME": {
    //                        "header": {
    //                            "KEY1": "VALUE1"
    //                            ...
    //                        },
    //                        "querystring": {
    //                            "KEY1": "VALUE1"
    //                            ...
    //                        },
    //                        "body": {
    //                            "KEY1": "VALUE1"
    //                            ...
    //                        },
    //                    },
    //                    ...
    //                } 
    responseFunc( epico.PullApiData( "./epico-configs/", "./epico-plugins/aws/aws.so", []string{"XXXAWS_ACCESS_KEYXXX", "XXXXXXXXXXXXAWS_SECRET_KEYXXXXXXXXXX"}, []string(nil), []string(nil), map[string]map[string]map[string]string(nil) ) )
}

func responseFunc( answer []byte ) {
    fmt.Printf("Answer: %v\n", string(answer))
}
```


Code Layout
---
`utils`: Utilities used by plugins for common API tasks such managing/parsing JSON/XML.  
`structs`: Basic structs representing common connection characteristics - ApiRequest, ApiResponse, etc.  
`signers`: Signers used by various APIs for security/auth.  
`sample.xml`: A sample API definition XML with the various options laid out.  


Anatomy of a Plugin
---
Plugins have three major interfaces to the Epico core:
1. The auth function which preapares our ApiRequest to Authenticate to the API
2. The paging peek function which looks at the response and determines if we need to page
3. The post process function which takes the API responses and parses them into a final JSON response []byte 

These need to be exported with the following names - PluginAuthFunction, PluginPostProcessFunction, and PluginPagingPeekFunction - like so:

```
// Function names are PluginAuth, PluginPostProcess, and PluginPagingPeek
var PluginAuthFunction = PluginAuth
var PluginPagingPeekFunction = PluginPagingPeek
var PluginPostProcessFunction = PluginPostProcess
```

The function signatures are as follows:

`PluginAuthFunction`: `func( generic_structs.ApiRequest, []string ) []byte`
The parameters are an ApiRequest, and a `[]string` containing auth parameters and any other plugin-specific configs.  The return is an `ApiRequest` that has been presigned/filled with credentials/otherwise prepared to run and be authenticated.

`PluginPagingPeekFunction`: `func( []byte, []string, interface{}, []string ) ( interface{}, bool )`
The parameters are the API response in `[]byte` form, the a `[]string` containing the split key from the `indicator_from_field` in the YAML paging section, and an `interface{}` representing the previous paging value/key, if any. The last value is a `[]string` of any plugin-specific configs.  The returns are an `interface{}` representing the new paging key and a `bool` indicating whether further paging is required.

`PluginPostProcessFunction`: `func( map[generic_structs.ComparableApiRequest][]byte, []map[string]string, []string ) []byte`
The parameters are a map of `ComparableApiRequests` and their associated `[]byte` API responses, and a list of API vars/keys associated with the requests made.  The last value is a `[]string` of any plugin-specific configs.  The return is a `[]byte` reprsenting the final JSON output.


Development Considerations
---
* Please use standard `utils` like the built-in logging functions to keep things consistent.
* Please contribute more widely reusable code to the core project rather than embedding it in your plugin. 


Future Improvements
---
* Appropriate testing.
* Support other request types beyond GET.
* Handle backoffs on requests.
* Leverage goroutines for efficiency.
* More idiomatic Go layout/formatting.
* Accessible from other languages. (https://medium.com/learning-the-go-programming-language/calling-go-functions-from-other-languages-4c7d8bcc69bf)
* Allow handling of errors separately/splitting into two JSON outputs?
