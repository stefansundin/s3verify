package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	flag "github.com/stefansundin/go-zflag"
)

const version = "0.1.0"

func init() {
	// Do not fail if a region is not specified anywhere
	// This is only used for the first call that looks up the bucket region
	if _, present := os.LookupEnv("AWS_DEFAULT_REGION"); !present {
		os.Setenv("AWS_DEFAULT_REGION", "us-east-1")
	}
}

func main() {
	var profile, region, endpointURL, caBundle, versionId string
	var noVerifySsl, noSignRequest, usePathStyle, debug, versionFlag, helpFlag bool
	flag.StringVar(&profile, "profile", "", "Use a specific profile from your credential file.")
	flag.StringVar(&region, "region", "", "The region to use. Overrides config/env settings. Avoids one API call.")
	flag.StringVar(&endpointURL, "endpoint-url", "", "Override the S3 endpoint URL. (for use with S3 compatible APIs)")
	flag.StringVar(&caBundle, "ca-bundle", "", "The CA certificate bundle to use when verifying SSL certificates.")
	flag.StringVar(&versionId, "version-id", "", "Version ID used to reference a specific version of the S3 object.")
	flag.BoolVar(&noVerifySsl, "no-verify-ssl", false, "Do not verify SSL certificates.")
	flag.BoolVar(&noSignRequest, "no-sign-request", false, "Do not sign requests. This does not work with Amazon S3, but may work with other S3 APIs.")
	flag.BoolVar(&usePathStyle, "use-path-style", false, "Use S3 Path Style.")
	flag.BoolVar(&debug, "debug", false, "Turn on debug logging.")
	flag.BoolVar(&versionFlag, "version", false, "Print version number.")
	flag.BoolVar(&helpFlag, "help", false, "Show this help.", flag.OptShorthand('h'))
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "s3verify version %s\n", version)
		fmt.Fprintln(os.Stderr, "Copyright (C) 2022 Stefan Sundin")
		fmt.Fprintln(os.Stderr, "Website: https://github.com/stefansundin/s3verify")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "s3verify comes with ABSOLUTELY NO WARRANTY.")
		fmt.Fprintln(os.Stderr, "This is free software, and you are welcome to redistribute it under certain")
		fmt.Fprintln(os.Stderr, "conditions. See the GNU General Public Licence version 3 for details.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <LocalPath> <S3Uri>\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "LocalPath can be - for stdin.")
		fmt.Fprintln(os.Stderr, "S3Uri must have the format s3://<bucketname>/<key>.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
	}
	flag.Parse()

	if versionFlag {
		fmt.Println(version)
		os.Exit(0)
	} else if helpFlag {
		flag.Usage()
		os.Exit(0)
	} else if flag.NArg() < 2 {
		flag.Usage()
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Error: LocalPath and S3Uri arguments are required!")
		os.Exit(1)
	} else if flag.NArg() > 2 {
		flag.Usage()
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Error: Too many positional arguments.")
		os.Exit(1)
	}

	if endpointURL != "" {
		if !strings.HasPrefix(endpointURL, "http://") && !strings.HasPrefix(endpointURL, "https://") {
			fmt.Fprintln(os.Stderr, "Error: the endpoint URL must start with http:// or https://.")
			os.Exit(1)
		}
		if !usePathStyle {
			u, err := url.Parse(endpointURL)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error: unable to parse the endpoint URL.")
				os.Exit(1)
			}
			hostname := u.Hostname()
			if hostname == "localhost" || net.ParseIP(hostname) != nil {
				if debug {
					fmt.Fprintln(os.Stderr, "Detected IP address in endpoint URL. Implicitly opting in for path style.")
				}
				usePathStyle = true
			}
		}
	}

	localPath := flag.Arg(0)
	bucket, key := parseS3Uri(flag.Arg(1))
	if bucket == "" || key == "" {
		fmt.Fprintln(os.Stderr, "Error: The S3Uri must have the format s3://<bucketname>/<key>")
		os.Exit(1)
	}

	// Open the file
	var f *os.File
	if localPath == "-" {
		f = os.Stdin
	} else {
		var err error
		f, err = os.Open(localPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
	}

	// Initialize the AWS SDK
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		func(o *config.LoadOptions) error {
			if profile != "" {
				o.SharedConfigProfile = profile
			}
			if caBundle != "" {
				f, err := os.Open(caBundle)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				o.CustomCABundle = f
			}
			if noVerifySsl {
				o.HTTPClient = &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{
							InsecureSkipVerify: true,
						},
					},
				}
			}
			if debug {
				var lm aws.ClientLogMode = aws.LogRequest | aws.LogResponse
				o.ClientLogMode = &lm
			}
			return nil
		},
		config.WithAssumeRoleCredentialOptions(func(o *stscreds.AssumeRoleOptions) {
			o.TokenProvider = mfaTokenProvider
		}),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if !noSignRequest {
		creds, _ := cfg.Credentials.Retrieve(context.TODO())
		if creds.AccessKeyID == "" {
			fmt.Fprintln(os.Stderr, "Warning: AWS credentials were not found. Please set up your AWS credentials.")
		}
	}

	client := s3.NewFromConfig(cfg,
		func(o *s3.Options) {
			if noSignRequest {
				o.Credentials = aws.AnonymousCredentials{}
			}
			if region != "" {
				o.Region = region
			}
			if endpointURL != "" {
				o.BaseEndpoint = aws.String(endpointURL)
			}
			if usePathStyle {
				o.UsePathStyle = true
			}
		})

	// Get the bucket location
	if endpointURL == "" && region == "" {
		bucketLocationOutput, err := client.GetBucketLocation(context.TODO(), &s3.GetBucketLocationInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			var ae smithy.APIError
			if errors.As(err, &ae) && ae.ErrorCode() == "AccessDenied" {
				fmt.Fprintln(os.Stderr, "\nYou can use --region to manually specify the bucket region.")
			}
			os.Exit(1)
		}
		bucketRegion := normalizeBucketLocation(bucketLocationOutput.LocationConstraint)
		if debug {
			fmt.Fprintf(os.Stderr, "Bucket region: %s\n", bucketRegion)
		}
		client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			if v, ok := os.LookupEnv("AWS_USE_DUALSTACK_ENDPOINT"); !ok || v != "false" {
				o.EndpointOptions.UseDualStackEndpoint = aws.DualStackEndpointStateEnabled
			}
			if noSignRequest {
				o.Credentials = aws.AnonymousCredentials{}
			}
			o.Region = bucketRegion
		})
	}

	fmt.Fprintln(os.Stderr, "Fetching S3 object information...")
	if debug {
		fmt.Fprintln(os.Stderr)
	}

	getObjectAttributesInput := &s3.GetObjectAttributesInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		ObjectAttributes: []s3Types.ObjectAttributes{
			s3Types.ObjectAttributesChecksum,
			s3Types.ObjectAttributesObjectParts,
			s3Types.ObjectAttributesObjectSize,
		},
		MaxParts: aws.Int32(100000),
	}
	if versionId != "" {
		getObjectAttributesInput.VersionId = aws.String(versionId)
	}
	objAttrs, err := client.GetObjectAttributes(context.TODO(), getObjectAttributesInput)
	if err != nil {
		if isSmithyErrorCode(err, 404) {
			fmt.Fprintln(os.Stderr, "Error: The object does not exist.")
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}

	if debug {
		fmt.Fprintln(os.Stderr, string(jsonMustMarshalSortedIndent(objAttrs, "", "  ")))
		fmt.Fprintln(os.Stderr)
	}

	if objAttrs.Checksum == nil {
		fmt.Fprintln(os.Stderr, "Error: This S3 object was not uploaded using the additional checksum feature. s3verify requires that the object is uploaded with this feature enabled. Please consult https://docs.aws.amazon.com/AmazonS3/latest/userguide/checking-object-integrity.html")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "You may also find s3sha256sum useful: https://github.com/stefansundin/s3sha256sum")
		os.Exit(1)
	}

	// Compare the file sizes if possible
	if localPath != "-" {
		stat, err := os.Stat(localPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fileSize := stat.Size()
		objectSize := aws.ToInt64(objAttrs.ObjectSize)
		if objectSize != fileSize {
			fmt.Fprintf(os.Stderr, "Error: The size of the S3 object (%d bytes) does not match the size of the local file (%d bytes).\n", objectSize, fileSize)
			os.Exit(1)
		}
	}

	algorithm, err := getChecksumAlgorithm(objAttrs.Checksum)
	if err != nil {
		fmt.Fprintln(os.Stderr, "This S3 object was uploaded using an unsupported checksum algorithm. Please file an issue: https://github.com/stefansundin/s3verify")
		os.Exit(1)
	}
	objSum, err := getChecksum(objAttrs.Checksum, algorithm)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	h, err := newHash(algorithm)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("S3 object checksum: %s\n", objSum)

	if objAttrs.ObjectParts == nil {
		// Not a multi-part object:
		_, err = io.Copy(h, f)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		sum := base64.StdEncoding.EncodeToString(h.Sum(nil))
		fmt.Println()
		fmt.Printf("Local file checksum: %s\n", sum)
		fmt.Println()
		if sum != objSum {
			fmt.Println("Checksum MISMATCH! File and S3 object are NOT identical!")
			os.Exit(1)
		}
		fmt.Println("Checksum matches! File and S3 object are identical.")
		os.Exit(0)
	}

	// A multi-part object:
	numParts := int(aws.ToInt32(objAttrs.ObjectParts.TotalPartsCount))
	fmt.Printf("Object consists of %d part%s.\n", numParts, pluralize(numParts))
	fmt.Println()

	if numParts != len(objAttrs.ObjectParts.Parts) || aws.ToBool(objAttrs.ObjectParts.IsTruncated) {
		fmt.Fprintln(os.Stderr, "This S3 object has more parts than were returned in the response. Please file an issue: https://github.com/stefansundin/s3verify")
		os.Exit(1)
	}

	partLengthDigits := 1 + int64(math.Floor(math.Log10(float64(numParts))))
	partFmtStr := fmt.Sprintf("Part %%%dd: %%s  ", partLengthDigits)

	var offset int64
	var partNumber int32 = 1
	for _, part := range objAttrs.ObjectParts.Parts {
		if partNumber != aws.ToInt32(part.PartNumber) {
			fmt.Fprintln(os.Stderr, "The parts of the S3 object are not sorted in the response. Please file an issue: https://github.com/stefansundin/s3verify")
			os.Exit(1)
		}

		partSize := aws.ToInt64(part.Size)
		partHash, err := newHash(algorithm)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_, err = io.Copy(partHash, io.LimitReader(f, partSize))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		partSum := partHash.Sum(nil)
		partSumEncoded := base64.StdEncoding.EncodeToString(partSum)
		fmt.Printf(partFmtStr, partNumber, partSumEncoded)
		partChecksum, err := getPartChecksum(&part, algorithm)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if partSumEncoded != partChecksum {
			fmt.Println("FAILED")
			fmt.Println()
			fmt.Printf("Local file did not match part %d (bytes %d to %d).\n", partNumber, offset, offset+partSize)
			os.Exit(1)
		}
		fmt.Println("OK")
		h.Write([]byte(partSum))
		offset += partSize
		partNumber++
	}

	sum := base64.StdEncoding.EncodeToString(h.Sum(nil))
	if len(sum) != len(objSum) {
		// Directory buckets add the number of parts to the end of the checksum of checksums, separated with a dash
		sum = fmt.Sprintf("%s-%d", sum, numParts)
	}
	fmt.Println()
	fmt.Printf("Checksum of checksums: %s\n", sum)
	fmt.Println()
	if sum != objSum {
		fmt.Println("Checksum MISMATCH! File and S3 object are NOT identical!")
		os.Exit(1)
	}
	fmt.Println("Checksum matches! File and S3 object are identical.")
	os.Exit(0)
}
