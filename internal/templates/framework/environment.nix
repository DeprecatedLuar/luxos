{ ... }:
let
  reservedKeys = [
    "HOME" "USER" "LOGNAME" "SHELL" "XDG_RUNTIME_DIR" "XDG_SESSION_ID"
    "XDG_SESSION_TYPE" "XDG_SEAT" "XDG_VTNR" "DISPLAY" "WAYLAND_DISPLAY"
    "DBUS_SESSION_BUS_ADDRESS" "PATH"
  ];

  # Token lists: a string is a literal, { ref = "HOME"; } is a $HOME / $USER reference.
  isLit = builtins.isString;

  fail = n: msg: throw "luxos environment: line ${toString n}: ${msg}";

  # Drop empty literals and merge adjacent ones.
  normalize = tokens:
    builtins.foldl' (acc: t:
      if isLit t && t == "" then acc
      else if isLit t && acc != [ ] && isLit (builtins.elemAt acc (builtins.length acc - 1))
      then (builtins.genList (i: builtins.elemAt acc i) (builtins.length acc - 1))
        ++ [ (builtins.elemAt acc (builtins.length acc - 1) + t) ]
      else acc ++ [ t ]) [ ] tokens;

  varRe = "[$]([{][A-Za-z_][A-Za-z0-9_]*[}]|[A-Za-z_][A-Za-z0-9_]*)";
  nameRe = "[{]?([A-Za-z_][A-Za-z0-9_]*)[}]?";

  # Tokens for an unquoted or double-quoted value.
  scan = n: vars: content:
    let
      parts = builtins.filter (p: p != "") (builtins.split varRe content);
      one = p:
        if isLit p then
          let bad = builtins.match "[^$\\`]*([$\\`]).*" p; in
          if bad == null then [ p ]
          else if builtins.elemAt bad 0 == "$" then fail n "invalid $"
          else fail n "backslash and backtick are only allowed in single quotes"
        else
          let name = builtins.elemAt (builtins.match nameRe (builtins.elemAt p 0)) 0; in
          if name == "HOME" || name == "USER" then [ { ref = name; } ]
          else if builtins.hasAttr name vars then vars.${name}
          else fail n "unknown variable ${name}";
    in
    builtins.concatLists (map one parts);

  contains = re: s: builtins.match ".*${re}.*" s != null;

  check = n: tokens:
    let
      lits = builtins.filter isLit tokens;
      any = pred: builtins.any pred lits;
      followed = builtins.foldl' (acc: t:
        { bad = acc.bad || (isLit t && acc.prevRef && builtins.match "[A-Za-z0-9_].*" t != null);
          prevRef = !(isLit t); }) { bad = false; prevRef = false; } tokens;
    in
    if any (contains "\"") then fail n "\" cannot be used in a value"
    else if any (contains "@[{]") then fail n "@{ cannot be used in a value"
    else if any (l: contains "[$]HOME" l || contains "[$]USER" l) then
      fail n "literal $HOME/$USER cannot be used in a value"
    else if followed.bad then fail n "$HOME/$USER must not be followed by a name character"
    else tokens;

  tail = "([[:space:]]+(#.*)?)?";

  parseValue = n: vars: rest:
    let
      sq = builtins.match "'([^']*)'${tail}" rest;
      dq = builtins.match "\"([^\"]*)\"${tail}" rest;
      uq = builtins.match "([^[:space:]'\"]*)${tail}" rest;
    in
    if sq != null then [ (builtins.elemAt sq 0) ]
    else if dq != null then scan n vars (builtins.elemAt dq 0)
    else if uq != null then scan n vars (builtins.elemAt uq 0)
    else fail n "invalid value";

  step = st: line:
    let
      n = st.n + 1;
      stripped = builtins.elemAt (builtins.match "[ \t]*(.*)" line) 0;
      exp = builtins.match "export[ \t]+(.*)" stripped;
      body = if exp != null then builtins.elemAt exp 0 else stripped;
      kv = builtins.match "([A-Za-z_][A-Za-z0-9_]*)=(.*)" body;
      key = builtins.elemAt kv 0;
      value =
        if builtins.elem key reservedKeys then fail n "${key} is reserved"
        else if builtins.hasAttr key st.vars then fail n "duplicate key ${key}"
        else check n (normalize (parseValue n st.vars (builtins.elemAt kv 1)));
    in
    if stripped == "" || builtins.substring 0 1 stripped == "#" then st // { inherit n; }
    else if kv == null then fail n "expected KEY=VALUE"
    else { inherit n; vars = st.vars // { ${key} = value; }; };

  encodeLit = builtins.replaceStrings [ "\\" "$" "`" ] [ "\\\\" "\\$" "\\`" ];
  encode = tokens:
    builtins.concatStringsSep "" (map (t: if isLit t then encodeLit t else "$" + t.ref) tokens);

  content = builtins.readFile ../config/environment;
  lines = builtins.filter isLit (builtins.split "\n" content);
  final = builtins.foldl' step { n = 0; vars = { }; } lines;
in
{
  environment.sessionVariables = builtins.mapAttrs (_: encode) final.vars;
}
