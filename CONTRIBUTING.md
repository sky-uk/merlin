# Contributing

We welcome any PRs and discussion on this project.

Please follow general Go conventions:
* https://golang.org/doc/effective_go.html
* https://github.com/golang/go/wiki/CodeReviewComments

All PRs should have tests for the accompanying code:
* Tests are written using Ginkgo/Gomega.
* Use [e2e](e2e/) for client/server CRUD operations.
* Use unit tests with mocks for external interactions with IPVS.
