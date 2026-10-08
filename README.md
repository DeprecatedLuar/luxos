```
nix --extra-experimental-features 'nix-command flakes' run github:DeprecatedLuar/luxos -- rebuild switch
```

On a computer with no luxos config this runs a short setup wizard first (`luxos setup` runs it on its own).
