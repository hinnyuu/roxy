{
  description = "roxy - LLM-agent-driven anime media library organizer";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
  };

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
      version = "0.0.0-dev";

      roxy = pkgs.buildGoModule {
        pname = "roxy";
        inherit version;
        src = ./.;
        # 依赖由 go.sum + vendorHash 锁定；vendor/ 不入库（docs/DECISIONS.md D-035）
        vendorHash = "sha256-i4+ylYyBwFb7um6OjJuufGI1/fcSTMMxCiIVXuqyTp4=";
        subPackages = [ "cmd/roxy" ];
        ldflags = [ "-X main.version=${version}" ];
        meta = {
          description = "LLM-agent-driven anime media library organizer";
          homepage = "https://github.com/hinnyuu/roxy";
          license = pkgs.lib.licenses.agpl3Only;
          mainProgram = "roxy";
        };
      };
    in
    {
      packages.${system} = {
        # 生产产物：静态单二进制，闭包只含运行时依赖
        default = roxy;

        # OCI 镜像（dockerTools 纯文件组装，无需 daemon；禁止 runAsRoot）
        image = pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/hinnyuu/roxy";
          tag = "dev";
          contents = [ pkgs.cacert ];
          config = {
            Entrypoint = [ "${roxy}/bin/roxy" ];
            Cmd = [ "serve" ];
            Env = [
              "ROXY_DATA_DIR=/data"
              "SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt"
            ];
            ExposedPorts = { "8080/tcp" = { }; };
            Volumes = { "/data" = { }; };
            Labels = {
              "org.opencontainers.image.source" = "https://github.com/hinnyuu/roxy";
              "org.opencontainers.image.licenses" = "AGPL-3.0";
              "org.opencontainers.image.title" = "roxy";
            };
          };
        };
      };

      # 开发环境：仅开发工具，不进生产闭包
      devShells.${system}.default = pkgs.mkShell {
        packages = with pkgs; [
          go
          gotools
          gopls
          nodejs
          skopeo
          jq
          sqlite
        ];
      };
    };
}
