# 0.2.2

* checkKey.key type modified to match generated code for `proto3` syntax

# 0.2.1

* Update alpine image to fix CVE-2019-5021.
* Update to go version 1.11

# 0.2.0

* Add support for etcd3 storage backend
* Add `store-backend` command line switch to allow storage backend to use etcd2 or etcd3. Defaults to etcd2.

# 0.1.4

* Add netlink timeouts to guard against a hanging reconciler.

# 0.1.3

* Update weights immediately when healthcheck state changes.
* Fix IPVS initialisation.
* Fix external updating of real server weights to 0.
* Improve log output.
* Add --version flag.

# 0.1.2

* Swap to github.com/docker/libnetwork for IPVS, to improve stability.

# 0.1.1

* Fix panics when updating with nil fields.

# 0.1.0

* Move health checks to the real server configuration.

# 0.0.1

* First alpha release of merlin.
