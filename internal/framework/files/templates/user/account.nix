{ pkgs, ... }:

{
  users.users.user = {
    isNormalUser = true;
    # extraGroups = [ "networkmanager" "wheel" ];
    openssh.authorizedKeys.keys = [
      # "ssh-ed25519 AAAA... you@host"
    ];
    packages = with pkgs; [
      # cowsay
    ];
  };
}
