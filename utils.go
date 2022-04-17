package main

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
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

func getChecksumAlgorithm(v *s3Types.Checksum) (s3Types.ChecksumAlgorithm, error) {
	if v.ChecksumSHA1 != nil {
		return s3Types.ChecksumAlgorithmSha1, nil
	} else if v.ChecksumSHA256 != nil {
		return s3Types.ChecksumAlgorithmSha256, nil
	} else if v.ChecksumCRC32 != nil {
		return s3Types.ChecksumAlgorithmCrc32, nil
	} else if v.ChecksumCRC32C != nil {
		return s3Types.ChecksumAlgorithmCrc32c, nil
	}
	return "", fmt.Errorf("unsupported checksum algorithm")
}

func getChecksum(v *s3Types.Checksum, algorithm s3Types.ChecksumAlgorithm) (string, error) {
	switch algorithm {
	case s3Types.ChecksumAlgorithmSha1:
		return *v.ChecksumSHA1, nil
	case s3Types.ChecksumAlgorithmSha256:
		return *v.ChecksumSHA256, nil
	case s3Types.ChecksumAlgorithmCrc32:
		return *v.ChecksumCRC32, nil
	case s3Types.ChecksumAlgorithmCrc32c:
		return *v.ChecksumCRC32C, nil
	default:
		return "", fmt.Errorf("unsupported checksum algorithm, %v", algorithm)
	}
}

func getPartChecksum(v *s3Types.ObjectPart, algorithm s3Types.ChecksumAlgorithm) (string, error) {
	switch algorithm {
	case s3Types.ChecksumAlgorithmSha1:
		return *v.ChecksumSHA1, nil
	case s3Types.ChecksumAlgorithmSha256:
		return *v.ChecksumSHA256, nil
	case s3Types.ChecksumAlgorithmCrc32:
		return *v.ChecksumCRC32, nil
	case s3Types.ChecksumAlgorithmCrc32c:
		return *v.ChecksumCRC32C, nil
	default:
		return "", fmt.Errorf("unsupported checksum algorithm, %v", algorithm)
	}
}

// https://github.com/aws/aws-sdk-go-v2/blob/c214cb61990441aa165e216a3f7e845c50d21939/service/internal/checksum/algorithms.go#L80-L95
func newHash(v s3Types.ChecksumAlgorithm) (hash.Hash, error) {
	switch v {
	case s3Types.ChecksumAlgorithmSha1:
		return sha1.New(), nil
	case s3Types.ChecksumAlgorithmSha256:
		return sha256.New(), nil
	case s3Types.ChecksumAlgorithmCrc32:
		return crc32.NewIEEE(), nil
	case s3Types.ChecksumAlgorithmCrc32c:
		return crc32.New(crc32.MakeTable(crc32.Castagnoli)), nil
	default:
		return nil, fmt.Errorf("unsupported checksum algorithm, %v", v)
	}
}
