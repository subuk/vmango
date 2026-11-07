# Installer hypervisor recipes

These sources prepare Linux libvirt hosts. They never install, configure or
restart the Vmango server. Vmango selects the recipe from freshly collected
Linux facts; Richt does not interpolate package backend names from facts.

The initial recipe matrix is Ubuntu 24.04 (`x86_64` and `aarch64`) and
RHEL-compatible 10 (`x86_64` and `aarch64`). A recipe's presence does not establish
live validation on every distribution or architecture. See the
[validation report](VALIDATION.md) for the actually tested combinations.

Ubuntu uses `libvirtd.socket`. RHEL-compatible 10 uses modular libvirt sockets
and `virtproxyd.socket` for the normal system libvirt connection. The recipes
preserve package versions already installed, enable/start declared sockets,
create the `libvirt` group when missing, and add a named Polkit rule permitting
that group to manage libvirt. Host-specific inventory adds only explicitly
approved directory paths and additive membership of the selected existing
runtime account. It does not replace existing primary groups, SSH keys or
authentication settings.

Before releasing a recipe change, publish its source commit, update
`richt.RecipeRevision` to that full immutable Git commit, and rebuild Vmango.
Generated inventories use raw HTTPS URLs at that revision plus SHA-256 hashes
of the embedded source bytes. A mismatched or unavailable remote source fails
preparation. Saved-plan application does not refetch recipes, although package
transactions still require their configured repositories.

Supply a local Richt executable through
`vmango install --richt /absolute/path/to/richt`, or place it on `PATH`.
Vmango does not require a specific Richt source revision or version string;
development builds are accepted. Richt must support the installer's commands,
facts output and saved-plan format `1.8`.

Vmango records the executable's reported version and SHA-256 in saved-plan
metadata and refuses a changed executable during execution or continuation.
Binary downloading is deliberately outside this delivery.
