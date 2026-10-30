This adds `luxos` to your shell:

```sh
nix --extra-experimental-features 'nix-command flakes' shell github:DeprecatedLuar/luxos
```

Nothing will happen unless you add a config AND rebuild the system+switch.

Then run `luxos setup` to start the setup wizard OR download your config into ~/.config/luxos/ 

For more info I have a bunch of documentation on `lux --help`

Once done you can rebuild with:

```sh
luxos rebuild switch
```
