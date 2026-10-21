# luxos
$ luxos <command> [args]

## rebuild
$ luxos rebuild [flags] <action> [nixos-rebuild args]
> Build the system from your config. <action> is nixos-rebuild's: switch, boot, test, build.
- --host <name> | Build another host | luxos rebuild --host laptop switch
- --config, -C <dir> | Use another config folder | luxos rebuild -C ~/myconfig switch
- --prune | Drop imports that no longer resolve | luxos rebuild --prune switch
- --backup-dir <dir> | Where to move unknown files found in /etc/nixos | luxos rebuild --backup-dir ~/etc-backup switch
- --goodbye-luxos <dir> | Leave luxos: build <dir> as plain NixOS | luxos rebuild --goodbye-luxos ~/plain switch
- -y, --yes | Skip confirmations

## module
$ luxos module <verb> <name>
- list [category] | List modules | luxos module list desktop
- add <category/name> | Create a module | luxos module add desktop/waybar --enable
- edit <name> | Open a module in your editor | luxos module edit waybar
- configure <name> | Edit a module's settings | luxos module configure waybar --machine
- enable <name>... | Turn modules on | luxos module enable waybar kitty
- disable <name>... | Turn modules off | luxos module disable waybar
- remove <name> | Delete a module | luxos module remove waybar -y
- rename <old> <new> | Rename a module | luxos module rename waybar bar
- --enable | add: enable it too
- --machine | enable, configure: only on this computer
- --flat | list: a flat list instead of a tree
- -y, --yes | Skip confirmations

## package
$ luxos package <list|package...> [flags]
- list | List the packages your config installs | luxos package list --flat
- <package>... | Show one or more packages | luxos package git curl
- --host <name> | Use another host
- --config, -C <dir> | Use another config folder
- --flat | list: each package once

## flake
$ luxos flake <list|input...|update> [flags]
- list | List inputs | luxos flake list --offline
- <input>... | Show one or more inputs | luxos flake nixpkgs home-manager
- update [input]... | Update inputs, all of them by default | luxos flake update nixpkgs
- --host <name> | Use another host
- --config, -C <dir> | Use another config folder
- --offline | list, show: skip the upstream check

## user
$ luxos user <verb> <name>
> Same verbs as module, for users.
- add <name> | Create a user | luxos user add alice --enable
- passwd <user> | Set a user's password | luxos user passwd alice
- key <verb> <user> | Manage a user's key on this computer | luxos user key add alice
- --enable | add: enable it too

## passwd
$ luxos user passwd <user>
> Set a user's password.

## setup
$ luxos setup [--config <dir>]
> Create a config, or add this computer to one. rebuild runs it when there is no config.

## key
$ luxos user key <verb> <user>
- list | List keys on this computer | luxos user key list
- add <user> | Store a key, typed or piped in | pass show alice-key | luxos user key add alice
- get <user> | Print a key | luxos user key get alice
- remove <user> | Delete a key | luxos user key remove alice -y
- -y, --yes | remove: skip the confirmation

## Flags
- --deadlock | Update luxos first, then run the command with the new binary
- --json | Lists and show commands: print JSON
- --raw | Lists and show commands: plain text, even on a terminal

## Aliases
- rb | rebuild
- m, modules | module list
- users | user list
- flakes | flake list
- update | flake update
- packages | package list
- passwd | user passwd
- key | user key
- keys | user key list
- list, edit, configure | module list, edit, configure
- enable, disable | module enable, disable
- remove, rename | module remove, rename
- ls, a, e, config | list, add, edit, configure
- 1, 0 | enable, disable
- rm, rn | remove, rename
- -v, --version | version
- -h, --help | help


# rebuild | how a rebuild works

## Commands
$ luxos rebuild [flags] <action> [nixos-rebuild args]
- --host <name> | Build another host
- --config, -C <dir> | Use another config folder
- --prune | Drop imports that no longer resolve
- --backup-dir <dir> | Where to move unknown files found in /etc/nixos
- --goodbye-luxos <dir> | Leave luxos: build <dir> as plain NixOS
- -y, --yes | Skip confirmations

## Before the build
> rebuild runs as root and asks for sudo itself.
> If the host's flake.lock pins another luxos, rebuild restarts as that version first.
> If this computer's selection does not enable boot, it asks before going on.
- Links | Points host and machine at the active host and this computer
- Selections | Fixes import lines that moved or broke; --prune drops the ones it can't
- Users | Checks enabled users' passwords and keys on this computer
- Settings | Brings settings files in line with each module's options.nix
- Hardware | Reads this computer's hardware into luxos.hardware

## The build
> Your config is copied into a new folder and built from there. Only after switch, boot or test succeeds does it replace /etc/nixos, so /etc/nixos always holds a system that built.
> Paths in host.nix resolve against the copy, not your folder. build and dry runs leave /etc/nixos untouched.
- /etc/nixos/config | The copy of your config
- /etc/nixos/framework | luxos' own files

## First rebuild
> With no config, rebuild runs setup first. Files in /etc/nixos that luxos doesn't own are moved to --backup-dir, never deleted.


# hierarchy | where your config lives
> Your config is in ~/.config/luxos. $LUXOS_CONFIG_DIR, then $XDG_CONFIG_HOME/luxos, win if set.

## Layout
- modules/ | Modules every host can use
- modules/host | Link to the active host's modules
- modules/machine | Link to this computer's modules
- environment | Session variables (luxos help environment)
- host | Link to the active host
- machine | Link to this computer
- .local/hosts/ | One folder per host
- .local/machines/ | One folder per computer

## Hosts
> A host is a folder in .local/hosts/, named after a hostname. It is a role: what the system should be. It holds host.nix (channel, timezone, locale), the modules only it has, what it enables, its settings and its flake.lock.

## Machines
> A machine is a folder in .local/machines/, one per physical computer, named by a key derived from its hardware. It holds that computer's hardware-configuration.nix, its boot module, and modules only it needs. One host can run on several computers; each brings its own hardware.

## Links
> host and machine point at the active host and this computer. rebuild sets them. Don't edit or commit them; luxos keeps them in .gitignore.


# modules | units, bundles and shadowing

## Units
> A module is a .nix file, or a folder with a default.nix. Any other folder is a category you can nest freely. Names are global: two modules can't share one.
- packages/dev.nix | Module dev
- services/tailscale/ | Module tailscale
- desktop/compositors/ | Categories only

## Selections
> Each host and each computer has a modules/default.nix listing what it enables. enable and disable edit these lists for you; editing them by hand works too.

## Using other modules
> Import another module by name, never by path.
- imports = luxos.modules [ "x11" ]; | Import module x11

## Bundles
> A folder module with its own modules/ folder. Everything outside modules/ is the bundle; each module inside is a submodule you enable on its own, as <bundle>/<name>. Enabling a submodule enables its bundle; disabling a bundle disables its submodules.
- eduardo/ | The bundle
- eduardo/modules/git.nix | Submodule eduardo/git

## Example
- luxos module enable alice/git | Enables alice/git, and so the alice bundle
- luxos module disable alice | Disables the bundle and all its submodules

## Shadowing
> A host module named like a shared one replaces it on that host. A machine module named like either replaces both on this computer. list shows the replacement underlined in the original's place.

## Scopes
- add host/<name> | Create a module only the active host has
- add machine/<name> | Create a module only this computer has
- enable --machine | Enable a shared module on this computer only


# inputs | declaring flake inputs
> Any module can declare the flake inputs it needs. luxos collects them from the modules a host builds and locks them in the host's flake.lock.

## Declaring
> The declaration must be plain values: it can't use pkgs, config or any other module argument.
- luxos.inputs.<name>.url = "<url>"; | Short form
- luxos.inputs.<name> = { url = "<url>"; inputs.nixpkgs.follows = "nixpkgs"; }; | Full form

## Example
- luxos.inputs.mytool.url = "github:someone/mytool"; | Declares the input
- environment.systemPackages = [ inputs.mytool.packages.${pkgs.system}.default ]; | Install its package

## nixpkgs
> Every host declares its channel in host.nix, and it is required:
- luxos.inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11"; | The host's channel

## Using
> Modules receive every input as the inputs argument.
- imports = [ inputs.<name>.nixosModules.default ]; | A module from an input
- inputs.<name>.packages.${pkgs.system}.default | A package from an input
- pkgs.<name>.<package> | A package from another nixpkgs, such as pkgs.unstable.neovim

## Names
> An input pulled in by another is addressed by path, such as parent/child. luxos itself is an input; updating it updates the tool.


# users | user folders, passwords and keys
> A user is a bundle in modules/users/, named after the user. luxos user add creates it.
> A host enables the user; the user enables its own modules, the same on every host. A user's modules cannot use luxos.modules or declare options.

## Folder
> luxos restores its three files if you change them. Set groups in the host's settings file.
- default.nix | luxos': imports account.nix and options.nix
- account.nix | luxos': the account, its password file and authorized_keys
- options.nix | luxos': the groups option
- .password.age | The encrypted password
- authorized_keys | Yours: SSH public keys that may log in, one per line, created empty
- modules/ | Yours: each file is a submodule
- modules/default.nix | Yours: the modules this user brings to every host
- modules/packages.nix | The user's packages, created with the user

## Example
- luxos user add alice --enable | Creates modules/users/alice and enables it
- luxos user passwd alice | Sets the password; the key is printed once
- luxos user key add alice | On another computer: store the saved key

## Passwords
> luxos user passwd <user> asks the password twice and saves its hash, encrypted with the user's key, to .password.age. The first time it creates the key and prints it once: save it.

## Keys
> A key decrypts a user's password on one computer. It lives in /var/lib/luxos/keys/<user>, readable by root only, never in your config.
> Every computer that builds the user needs the key: luxos user key add <user>. Without it the account stays locked.


# markers | what list symbols mean

## Modules
- ◉ | Green: enabled and running
- ⊕ | Teal: enabled, applied on the next rebuild
- ⊕ | Blue: running, but its files changed since
- ⊘ | Red: running, but no longer enabled
- ⊘ | Struck red: running, but deleted from the config
- ◍ | Purple: pulled in by another module
- ○ | Off
- ◈ | A bundle, in its state's color, with its count of enabled submodules

## After a module name
- ❄ | Declares flake inputs
- ! | Fails to evaluate
- ⚙ | Has settings; yellow when they break a rule

## Inputs
- ◉ | Declared and locked
- ⊕ | Declared, locked on the next rebuild
- ⊘ | Locked, dropped on the next rebuild
- ◍ | Pulled in by another input
- ↑ | After a name: upstream has newer commits
- ? | After a name: upstream couldn't be checked

## Packages
- ◉ | Installed and in the config
- ⊕ | In the config, installed on the next switch
- ⊘ | Installed, removed on the next switch
- ❄ | After a name: from an input other than nixpkgs


# hardware | facts luxos reads from this computer
> Every rebuild reads this computer's hardware and passes it to modules as luxos.hardware. Read them; never set them.

## Facts
- luxos.hardware.vendor | Maker, such as "LENOVO"
- luxos.hardware.product | Model name
- luxos.hardware.chassisType | 3 desktop; 8, 9, 10, 14 laptop; 31 convertible
- luxos.hardware.cpuVendor | "intel", "amd" or "unknown"
- luxos.hardware.platformProfiles | Power profiles the firmware offers, or []
- luxos.hardware.gpus | Each GPU: busId, vendor, vendorId, deviceId, class, bootVga

## Examples
- services.thermald.enable = config.luxos.hardware.cpuVendor == "intel"; | Intel only
- lib.mkIf (lib.elem config.luxos.hardware.chassisType [ 8 9 10 14 ]) { ... } | Laptops only

## Done for you
> With an NVIDIA GPU, luxos enables its driver; with an NVIDIA and an integrated GPU, PRIME offload. Intel GPUs get video acceleration. All are defaults a module can override. Old NVIDIA cards that need a legacy driver get a warning instead.


# settings | module options and settings files
> A module can declare options in an options.nix next to its default.nix. luxos then keeps a settings file per host with every option, where you set the values.

## options.nix
- { lib, ... }: | Takes only lib
- options.<module>.<key> | One level under the module's name; eduardo/git uses options.eduardo.git.<key>
- type, default | Both required; the default is a plain value
- description | Optional; becomes the comment after the key in the settings file
- imports = [ ./options.nix ]; | In the module's default.nix

## Example: options.nix
- { lib, ... }: { | Takes only lib
- options.greeter.name = lib.mkOption { | One level under the module's name
- type = lib.types.str; | Required
- default = "world"; | Required, a plain value
- description = "who to greet"; | The comment in the settings file
- }; } | End of the option

## Example: default.nix
- imports = [ ./options.nix ]; | Pulls in the declaration
- environment.etc.hi.text = config.greeter.name; | Reads the value

## Example: the settings file
> luxos writes this to .local/hosts/<host>/settings/greeter.nix. Edit the value; the next rebuild uses it.
- { name = "world"; # who to greet } | The default, with its description

## Settings files
> Created on enable or rebuild with every default. New keys are appended; your lines are never rewritten. Keys no longer declared are commented out with a warning. luxos module configure <name> opens it; --machine creates the computer's.
> A broken options.nix or settings file stops the rebuild and shows ⚙ in yellow.
- .local/hosts/<host>/settings/<module>.nix | Per host
- .local/machines/<key>/settings/<module>.nix | Per computer; wins over the host's


# environment | the environment file
> CONFIG_DIR/environment sets session variables on every host.

## Syntax
- KEY=VALUE | One per line; blank lines and # comments are ignored
- export KEY=VALUE | export is accepted
- KEY="value" | Spaces allowed, $ references expand
- KEY='value' | Everything literal
- KEY=value # note | Comment after a space
- $HOME, $USER | Expanded per user at login
- $KEY, ${KEY} | A key defined earlier in the file

## Example
- # editor and paths | A comment
- EDITOR=nvim | Plain value
- export PAGER=less | export is accepted
- PROJECTS="$HOME/code" | $HOME is expanded per user
- BIN="$PROJECTS/bin" | A key defined above
- GREETING='hello $USER' | Single quotes: kept literally

## Fails the build
> A module setting the same key to another value fails the build; lib.mkForce in the module wins. Your shell rc runs later and wins in that shell. Delete the file and the next rebuild restores it; empty it to set nothing.
- Reserved keys | HOME USER LOGNAME SHELL PATH DISPLAY WAYLAND_DISPLAY DBUS_SESSION_BUS_ADDRESS XDG_RUNTIME_DIR XDG_SESSION_ID XDG_SESSION_TYPE XDG_SEAT XDG_VTNR
- Unknown $VAR | Only $HOME, $USER and earlier keys
- \ or backtick | Outside single quotes
- " or @{ | Anywhere in a value
- Duplicate key | Each key once
- Other syntax | Spaces around =, unquoted spaces, mixed quotes
