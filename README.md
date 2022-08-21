s3verify is a program that can verify that a local file is identical to an object on Amazon S3, without having to download the object.

The S3 objects must be uploaded using the [Additional Checksum Algorithms feature released in February 2022](https://aws.amazon.com/blogs/aws/new-additional-checksum-algorithms-for-amazon-s3/). For objects that weren't uploaded using that you might find [s3sha256sum](https://github.com/stefansundin/s3sha256sum) useful instead.

## Installation

Precompiled binaries will be provided at a later date. For now you can install using `go install`:

```
go install github.com/stefansundin/s3verify@latest
```

## Usage

```
$ s3verify --help
Usage: s3verify [options] <LocalPath> <S3Uri>
S3Uri must have the format s3://<bucketname>/<key>.

Options:
      --ca-bundle string      The CA certificate bundle to use when verifying SSL certificates.
      --debug                 Turn on debug logging.
      --endpoint-url string   Override the S3 endpoint URL. (for use with S3 compatible APIs)
  -h, --help                  Show this help.
      --no-sign-request       Do not sign requests.
      --no-verify-ssl         Do not verify SSL certificates.
      --profile string        Use a specific profile from your credential file.
      --region string         The region to use. Overrides config/env settings. Avoids one API call.
      --use-path-style        Use S3 Path Style.
      --version               Print version number.
      --version-id string     Version ID used to reference a specific version of the S3 object.
```
