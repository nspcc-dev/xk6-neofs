# Changelog

This document outlines major changes between releases.

## [Unreleased]

### Added

### Fixed

### Changed
- Go 1.24+ is required to build now (#101)

### Updated
- github.com/nspcc-dev/tzhash v1.8.2 => v1.8.3 (#101)
- github.com/nspcc-dev/neofs-sdk-go v1.0.0-rc.13 => v1.0.0-rc.14 (#101)
- github.com/aws/aws-sdk-go-v2 v1.36.3 => v1.39.0 (#101)
- github.com/aws/aws-sdk-go-v2/config v1.29.9 => v1.31.7 (#101)
- github.com/aws/aws-sdk-go-v2/service/s3 v1.78.2 => v1.88.0 (#101)

## [0.2.0] - 2025-03-20

### Added
- 'Search' operation test (#91)
- darwin/arm64 binaries (#96)
- Terce result output (#93)

### Fixed
- Concurrent data generation (#93)

### Changed
- Go 1.23+ is required to build now (#61, #97, #103)
- Replaced `math/rand.Read` with `math/rand/v2.ChaCha8.Read`

### Updated
- google.golang.org/protobuf dependency to 1.33.0 (#83)
- golang.org/x/net dependency to 0.23.0 (#84)
- xk6 version to 0.11.0 (#87)
- k6 version to 0.51.0 (#87)
- NeoFS SDK dependency to RC13 (#95, #103)
- NeoGo dependency to 0.108.1 (#95, #103)
- github.com/nspcc-dev/tzhash dependency to 1.8.2 (#95, #103)
- github.com/aws/aws-sdk-go-v2 dependency to 1.36.3 (#95, #103)
- go.etcd.io/bbolt dependency to 1.3.11 (#95, #103)
- golang.org/x/crypto dependency to 0.31.0 (#100)

## [0.1.2] - 2024-03-11

### Added
- Support for zero-size objects for upload (#80)

### Changed
- `bbolt`: Disabled syncing the DB in the object registry (#67)
- Bump `golang.org/x/net` from 0.15.0 to 0.17.0 (#69)
- Bump `google.golang.org/grpc` from 1.58.0 to 1.58.3 (#70)
- Bump `golang.org/x/crypto` from 0.14.0 to 0.17.0 (#71)
- Upgraded Go version to a minimum 1.20 and updated versions for GitHub Actions and workflows (#77, #78, #79)


## Older versions

Please refer to [GitHub releases](https://github.com/nspcc-dev/xk6-neofs/releases/) for older releases.

[0.1.2]: https://github.com/nspcc-dev/xk6-neofs/compare/v0.1.1...v0.1.2
[0.2.0]: https://github.com/nspcc-dev/xk6-neofs/compare/v0.1.2...v0.2.0
[Unreleased]: https://github.com/nspcc-dev/xk6-neofs/compare/v0.2.0...master
