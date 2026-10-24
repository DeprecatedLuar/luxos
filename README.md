This adds `luxos` to your shell:

```sh
nix --extra-experimental-features 'nix-command flakes' shell github:DeprecatedLuar/luxos
```

Then download your configuration and run it:

```sh
luxos rebuild switch
```
