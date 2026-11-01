# The blog is now the default application

The executable moved to [cmd/web](../../cmd/web), and the module moved to [modules/blog](../../modules/blog). Follow the [blog guide](../../docs/blog/readme.md) for setup, upgrades, and maintenance.

Existing local configuration, databases, uploads, and assets in this directory have not moved. Build the executable from the repository root, then run it from the existing configuration directory. Review the guide's database upgrade requirements before starting it. Do not replace existing configuration with fresh-install templates.
