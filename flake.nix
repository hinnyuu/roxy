{
  description = "roxy - LLM-agent-driven anime media library organizer";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
  };

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
      lib = pkgs.lib;
      version = "0.0.0-dev";

      # 源过滤：不可变产物不烘焙开发/运行时状态（AGENTS.md 打包纪律）
      srcFiltered = lib.cleanSourceWith {
        src = ./.;
        filter = path: type:
          let
            rel = lib.removePrefix (toString ./. + "/") (toString path);
            base = baseNameOf path;
            excluded =
              lib.hasPrefix "web/node_modules" rel ||
              lib.hasPrefix "web/dist" rel ||
              lib.hasPrefix "vendor" rel ||
              lib.hasPrefix "data" rel ||
              lib.hasPrefix "test_share" rel ||
              lib.hasPrefix "bin" rel ||
              base == "result" || lib.hasPrefix "result-" base ||
              base == ".direnv" ||
              lib.hasSuffix ".db" base || lib.hasSuffix ".db-wal" base || lib.hasSuffix ".db-shm" base;
          in
          !excluded && lib.cleanSourceFilter path type;
      };

      # 前端产物（D-041）：buildNpmPackage 锁定依赖，go:embed 经 -tags=web 注入
      webui = pkgs.buildNpmPackage {
        pname = "roxy-web";
        inherit version;
        src = lib.cleanSourceWith {
          src = ./web;
          filter = path: type:
            let
              rel = lib.removePrefix (toString ./web + "/") (toString path);
            in
            !(lib.hasPrefix "node_modules" rel || lib.hasPrefix "dist" rel) &&
            lib.cleanSourceFilter path type;
        };
        # package-lock.json + npmDepsHash 锁定（与 go.sum + vendorHash 同构，D-035）
        npmDepsHash = "sha256-ufz4/nVgCGrh8xyDDGqRoFdKJE5oFP5qyxG3QQv9Wvc=";
        npmBuildScript = "build";
        installPhase = ''
          runHook preInstall
          mkdir -p $out
          cp -r dist $out/
          runHook postInstall
        '';
      };

      roxy = pkgs.buildGoModule {
        pname = "roxy";
        inherit version;
        src = srcFiltered;
        # 依赖由 go.sum + vendorHash 锁定；vendor/ 不入库（docs/DECISIONS.md D-035）
        vendorHash = "sha256-i4+ylYyBwFb7um6OjJuufGI1/fcSTMMxCiIVXuqyTp4=";
        subPackages = [ "cmd/roxy" ];
        tags = [ "web" ];
        ldflags = [ "-X main.version=${version}" ];
        preBuild = ''
          rm -rf web/dist
          cp -r ${webui}/dist web/dist
          chmod -R u+w web/dist
        '';
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
        # 生产产物：静态单二进制（内嵌前端），闭包只含运行时依赖
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
