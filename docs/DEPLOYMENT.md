# Run & Deploy

## Prerequisites

Go **1.23 or newer** for direct execution; Docker for the image-based path. The Dockerfile pins the builder to Go 1.27.2 and generates a minimal `scratch` image. No external Go modules or npm packages are required.

## Run locally without Docker

```bash
go run ./cmd/server
# Open http://localhost:8080
```

Configure the listen port and optional scoring sensitivity:

```bash
PORT=9090 DISPATCH_PENALTY_KM_EQ=20 go run ./cmd/server
```

## Run with Docker

```bash
docker build -t warehouse-recommendation:latest .
docker run --rm --name warehouse-recommendation -p 8080:8080 warehouse-recommendation:latest
```

The Docker image runs one Go process as non-root UID 65532. Frontend, catalog, and history dataset are compiled into the binary; no volume or database is needed. Use the hosting platform's HTTP health check at `/healthz` — the scratch image intentionally does not contain curl or a shell.

For production use HTTPS at the managed hosting edge or through a host reverse proxy such as Caddy/Nginx, and set the appropriate host/container restart policy. Keep the service reachable for the required review period. Do not expose Go's plain HTTP listener directly as an HTTPS endpoint.

## Test and benchmark

```bash
go test ./...
go test -race ./...
go vet ./...
go test -run '^$' -bench . -benchmem ./internal/selection
```

Smoke test:

```bash
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/api/v1/catalog
curl -fsS http://localhost:8080/api/v1/insights
curl -fsS -X POST http://localhost:8080/api/v1/recommendations \
  -H 'Content-Type: application/json' \
  -d '{"items":[{"product_id":"P002","quantity":45}]}'
```

Docker build/run must be verified on the target VPS; the local environment does not have the Docker CLI.

## VPS privat + Cloudflare Tunnel (Ubuntu 24.04)

Alur: browser HTTPS → Cloudflare → cloudflared di VPS → app:8080.
Tunnel membuat koneksi keluar dari VPS, sehingga tidak memerlukan IP publik
atau port forwarding HTTP/HTTPS. Pastikan domain berstatus Active di Cloudflare
dan nameserver sudah memakai Cloudflare. Contoh hostname: `simpul-test.bagasrn.site`.

### 1. Install Docker di VPS

Masuk dari terminal lokal:

```bash
ssh root@103.179.67.10 -p 2241
```

Jalankan di VPS (repository resmi Docker untuk Ubuntu Noble amd64):

```bash
apt-get update
apt-get install -y ca-certificates curl
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
cat > /etc/apt/sources.list.d/docker.sources <<'EOF'
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: noble
Components: stable
Architectures: amd64
Signed-By: /etc/apt/keyrings/docker.asc
EOF
apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
systemctl enable --now docker
docker compose version
mkdir -p /opt/simpul
exit
```

### 2. Upload project dan jalankan aplikasi

Dari terminal lokal, pada direktori project:

```bash
tar --exclude=.git --exclude=.env --exclude='.env.*' --exclude=.DS_Store -czf - . | ssh -p 2241 root@103.179.67.10 'tar -xzf - -C /opt/simpul'
ssh root@103.179.67.10 -p 2241
cd /opt/simpul
docker compose up -d --build app
curl -fsS http://127.0.0.1:8080/healthz
```

Hasil health check harus `{"status":"ok"}`. Port aplikasi hanya dibind ke
loopback VPS. Build menjalankan `go test ./...` sebelum membuat binary.

### 3. Buat tunnel di Cloudflare

1. Buka Cloudflare Zero Trust → Networks → Connectors / Tunnels (nama menu bisa berubah).
2. Create tunnel → Cloudflared → beri nama `simpul-vps`.
3. Pilih Docker. Ambil hanya nilai token dari perintah `--token ...` yang ditampilkan.
4. Simpan token langsung di VPS menggunakan editor, bukan di chat atau repository:

```bash
cd /opt/simpul
install -m 600 /dev/null .env
nano .env
```

Isi file:

```dotenv
TUNNEL_TOKEN=TOKEN_DARI_CLOUDFLARE
```

Jangan ulangi perintah `install` jika `.env` sudah berisi token, karena akan
mengosongkan file. Jalankan connector:

```bash
docker compose --profile tunnel up -d
docker compose logs --tail=50 cloudflared
```

Tunggu connector berstatus Healthy/Connected di Cloudflare.

### 4. Hubungkan hostname

Tambahkan **Published application route / Public hostname** pada tunnel:

| Field | Nilai contoh |
| --- | --- |
| Subdomain | `simpul` |
| Domain | domain milikmu, misalnya `example.com` |
| Path | kosong |
| Service type | `HTTP` |
| Service URL | `app:8080` |

Alamat origin lengkap adalah `http://app:8080`. Gunakan nama service `app`
karena connector dan aplikasi berada dalam network Compose yang sama;
`localhost` di container connector menunjuk ke container connector sendiri.
Simpan route dan pastikan record DNS tunnel berhasil dibuat. Jika hostname
sudah mempunyai record DNS lain, periksa pemakaiannya sebelum menggantinya.
Jangan arahkan record A ke IP privat VPS.

### 5. Verifikasi dan update

Dari laptop:

```bash
curl -fsS https://simpul-test.bagasrn.site/healthz
```

Buka `https://simpul-test.bagasrn.site` untuk mencoba antarmuka. HTTPS publik
ditangani Cloudflare; origin di network Docker memakai HTTP.
Aplikasi menjadi publik; gunakan Cloudflare Access jika hanya reviewer atau
tim tertentu yang boleh mengaksesnya.

Setelah upload perubahan kode, jalankan di VPS:

```bash
cd /opt/simpul
docker compose --profile tunnel up -d --build
docker compose ps
curl -fsS http://127.0.0.1:8080/healthz
```

Jika 502, cek `docker compose logs --tail=100 app cloudflared` dan URL origin
`http://app:8080`. Jika connector tidak terhubung, periksa token dan akses
keluar VPS ke Cloudflare pada port 7844 UDP (QUIC) atau TCP (HTTP/2).
Restart policy menghidupkan container kembali setelah reboot selama Docker aktif.

Referensi: [Docker Ubuntu](https://docs.docker.com/engine/install/ubuntu/),
[Cloudflare Tunnel](https://developers.cloudflare.com/tunnel/get-started/).

## Repository and submission

The confidential challenge brief is not included in the source tree. The required English README for the submission email is a **separate attachment** supplied alongside the source ZIP, not part of this repository. Prefer a private repository with reviewer access because this sample catalog and history originate from the challenge materials.

## Deployment Simpul di VPS Bagas

Hostname publik yang disiapkan: `simpul-test.bagasrn.site`.
Repository lokal dan VPS: `https://github.com/bagasrna/warehouse-recommendation.git`.
Checkout VPS berada di `/opt/simpul`, branch `main`.

Setelah aplikasi aktif, selesaikan konfigurasi Cloudflare:

1. Buat tunnel Cloudflared bernama `simpul-vps` dan ambil token connector.
2. Jalankan di VPS (input token disembunyikan dan tidak masuk shell history):

```bash
cd /opt/simpul
umask 077
read -r -s -p 'Cloudflare tunnel token: ' SIMPUL_TUNNEL_TOKEN
printf '\n'
printf 'TUNNEL_TOKEN=%s\n' "$SIMPUL_TUNNEL_TOKEN" > .env
unset SIMPUL_TUNNEL_TOKEN
docker compose --profile tunnel up -d
```

3. Tambahkan Published application route dengan subdomain `simpul-test`,
   domain `bagasrn.site`, service HTTP, URL `app:8080`, path kosong.
4. Cek `https://simpul-test.bagasrn.site/healthz` dan halaman utama.

Update berikutnya dari laptop:

```bash
git add .
git commit -m "Describe changes"
git push origin main
```

Lalu di VPS:

```bash
cd /opt/simpul
git pull --ff-only origin main
docker compose up -d --build app
curl -fsS http://127.0.0.1:8080/healthz
```

Container tunnel yang sudah aktif tetap berjalan saat service app diperbarui.
File `.env` disimpan hanya di VPS dan diabaikan Git serta Docker build context.
