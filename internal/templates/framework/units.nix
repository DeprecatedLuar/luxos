# The luxos.modules function (implementation-plan.md F6): given a modules
# root, returns a function from a list of names to the list of paths those
# names resolve to. A directory holding default.nix is a unit named after
# the directory; any other directory is a transparent category walked
# through; a *.nix file other than default.nix is a unit named after the
# file minus ".nix". The root's own default.nix never becomes a unit
# (walking it as a directory entry is always excluded by the same "not
# default.nix" rule that excludes it everywhere else).
{ lib, root }:
let
  # unitFor turns one directory entry into a name -> path attrset (zero or
  # one entry for a *.nix file / a unit directory), or recurses into it via
  # walk when it is a transparent category directory.
  unitFor = dir: name: entryType:
    let
      path = dir + "/${name}";
    in
    if entryType == "directory" then
      if builtins.pathExists (path + "/default.nix") then
        { ${name} = path; }
      else
        walk path
    else if entryType == "regular" && lib.hasSuffix ".nix" name && name != "default.nix" then
      { ${lib.removeSuffix ".nix" name} = path; }
    else
      { };

  # mergeUnit folds one directory entry's unitFor result into the
  # accumulator, throwing on any name already claimed anywhere in the tree.
  mergeUnit = dir: acc: name:
    let
      entries = builtins.readDir dir;
      found = unitFor dir name entries.${name};
    in
    lib.foldlAttrs
      (acc': unitName: unitPath:
        if acc' ? ${unitName} then
          throw "luxos units: duplicate module name '${unitName}' (${toString acc'.${unitName}}, ${toString unitPath})"
        else
          acc' // { ${unitName} = unitPath; }
      )
      acc
      found;

  # walk : path -> attrs of name -> path, over one directory's entries.
  walk = dir:
    lib.foldl' (mergeUnit dir) { } (builtins.attrNames (builtins.readDir dir));

  units = walk root;
in
names:
map
  (n: units.${n} or (throw "luxos.modules: '${n}' does not resolve to any module under modules/"))
  names
