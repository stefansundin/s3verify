package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

func pluralize(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Anyone know of a cleaner way to do this? :)
func jsonMustMarshalSortedIndent(v interface{}, prefix, indent string) []byte {
	// Uhh.. Marshal and then Unmarshal to sort the keys
	bytes, err := json.Marshal(v)
	if err != nil {
		return []byte("json.Marshal error: " + err.Error())
	}
	var data interface{}
	err = json.Unmarshal(bytes, &data)
	if err != nil {
		return []byte("json.Unmarshal error: " + err.Error())
	}
	// Then Marshal again :)
	output, err := json.MarshalIndent(data, prefix, indent)
	if err != nil {
		return []byte("json.MarshalIndent error: " + err.Error())
	}
	return output
}

func parseS3Uri(s string) (string, string) {
	if !strings.HasPrefix(s, "s3://") {
		return "", ""
	}
	parts := strings.SplitN(s[5:], "/", 2)
	if len(parts) == 0 {
		return "", ""
	} else if len(parts) == 1 {
		return parts[0], ""
	} else {
		return parts[0], parts[1]
	}
}

// https://github.com/aws/aws-sdk-go/blob/e2d6cb448883e4f4fcc5246650f89bde349041ec/service/s3/bucket_location.go#L15-L32
// Would be nice if aws-sdk-go-v2 supported this.
func normalizeBucketLocation(loc s3Types.BucketLocationConstraint) string {
	if loc == "" {
		return "us-east-1"
	}
	return string(loc)
}

func mfaTokenProvider() (string, error) {
	for {
		fmt.Printf("Assume Role MFA token code: ")
		var code string
		_, err := fmt.Scanln(&code)
		if len(code) == 6 && isNumeric(code) {
			return code, err
		}
		fmt.Println("Code must consist of 6 digits. Please try again.")
	}
}

func isSmithyErrorCode(err error, code int) bool {
	var re *smithyhttp.ResponseError
	if errors.As(err, &re) && re.HTTPStatusCode() == code {
		return true
	}
	return false
}
