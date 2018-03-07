# Design

## types
The merlin data structures live in [types/types.proto](). The requirements for them are:

* Provide a good representation of actual and desired state of IPVS. So they should be relatively similar
to the actual IPVS data structures.
* Structured in a way we can make updates to the store concurrently without complex locking required. Most
importantly, services and servers should be modifiable independently.

