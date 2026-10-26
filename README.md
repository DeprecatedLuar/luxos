This adds `luxos` to your shell:

```sh
nix --extra-experimental-features 'nix-command flakes' shell github:DeprecatedLuar/luxos --command bash -c 'luxos help; exec bash'
```

Then download your configuration and run it:

```sh
luxos rebuild switch
```
