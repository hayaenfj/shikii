[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · **Bahasa Indonesia**

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: mengubah "mungkin" dari AI menjadi "aman untuk dijalankan" yang bisa Anda pakai menjalankan bisnis

### Alat untuk membiarkan AI memutuskan ya/tidak, yang mana, atau seberapa banyak, dan menjaga keputusan itu tetap bisa dipercaya dalam pekerjaan sehari-hari. Anda memeriksa sebagian jawaban dan, sesekali, mengonfirmasi "apakah ini masih yang saya maksud?". Sisanya dikerjakan mesin.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-190%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[Masalahnya](#masalahnya) · [Apa yang dilakukan shikii](#apa-yang-dilakukan-shikii) · [Apa yang Anda lakukan](#apa-yang-anda-lakukan) · [Instalasi](#instalasi) · [Jalan masuk tersingkat](#jalan-masuk-tersingkat) · [Satu putaran penuh](#satu-putaran-penuh) · [Apa yang baru](#apa-yang-baru) · [Baca lebih lanjut](#baca-lebih-lanjut)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii: model memberi Anda sebuah probabilitas, shikii memberi tahu apakah Anda boleh bertindak atasnya" width="100%"></p>

## Masalahnya

Tanyakan pada AI "apakah pertanyaan ini perkara yang sama dengan yang sebelumnya?" dan ia menjawab "mungkin, 0,9". Untuk pekerjaan nyata, itu belum cukup.

- Apakah 0,9 itu benar-benar tepat 9 dari 10 kali?
- Sejak hari Anda menggantinya dengan model baru, apakah ia diam-diam mulai keliru?
- Kalau Anda mengubah sedikit kata-katanya, bagaimana akurasinya bergeser?
- Keputusan yang ia ambil sendiri, tanpa ada yang mengawasi: apakah masih benar?

Kalau seseorang harus memeriksa setiap jawaban, tidak banyak gunanya memakai AI.

## Apa yang dilakukan shikii

1. **Mengumpulkan koreksi Anda dan menarik garis batas yang di atasnya AI boleh bertindak sendiri.** Dari jawaban yang Anda beri ("ini yang sebenarnya benar") ia mengukur hubungan antara keyakinan AI dan seberapa sering AI benar, lalu memutuskan dari mana ke atas ia bisa dipercaya. Sebelum ada garis, tidak ada yang bertindak. Setelah ada, setiap keputusan otomatis membawa tanda terima yang menyebutkan garis itu bertumpu pada berapa jawaban yang sudah diperiksa dan seberapa sering jawaban itu benar
2. **Mencoba cara bertanya lain dan menyimpan yang lebih baik.** Pada koreksi yang sama ia menilai pertanyaan alternatif, deskripsi pilihan dan tata letak prompt, dan hanya mengadopsinya kalau benar-benar lebih baik. Anda cukup mengonfirmasi bahwa maknanya tidak berubah
3. **Membiarkan Anda mengganti model tanpa mulai dari nol.** Koreksi tersimpan terpisah dari model, sehingga model baru memakainya kembali dan garis ditarik ulang oleh mesin. Tidak perlu kerja manusia
4. **Menyimpan apa yang dipelajari dalam sebuah wiki.** "Bertanya dengan cara ini berhasil" terkumpul sebagai klaim dengan angka hasil ukur, yang bisa diuji ulang pada model lain. Apa yang orang lain ukur pada model mereka ditambahkan ke halaman yang sama

## Apa yang Anda lakukan

- **Tuliskan apa yang ingin diputuskan.** Jawab beberapa pertanyaan dan sebuah berkas keputusan muncul (`shikii new`, secara interaktif atau dengan `--type --question --option --input --pii`; format lengkapnya ada di [referensi spec](../../skills/shikii/references/spec.md))
- **Periksa jawaban.** Halaman menampilkan keputusan yang AI tidak yakin, plus 1 dari 20 keputusan yang ia jalankan sendiri, dan Anda menekan tombol untuk apa yang sebenarnya benar. Tombol 1 sampai 9 dan Enter (sama dengan pilihan AI) membuatnya cepat. "Sudah dijawab" memungkinkan Anda memperbaiki jawaban sebelumnya (yang terbaru yang dihitung). "Detail" menambahkan "tanya AI lagi", yang memutuskan ulang teks yang sama dengan kata-kata dan garis hari ini dan menampilkan sebelum dan sesudah; catatan hanya ditambahkan, tidak pernah dihapus
- **Sesekali konfirmasi "apakah ini masih yang saya maksud?".** Ketika mesin menemukan kata-kata yang nilainya lebih baik, Anda melihat apakah maknanya berubah dan menekan terima atau tolak

Semua yang lain adalah pekerjaan mesin.

## Instalasi

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # butuh Go 1.27 atau lebih baru
cd <root proyek> && shikii init
```

`shikii init` menyiapkan satu folder, `.shikii/` (berisi `decisions/` untuk spec Anda, `wiki/` untuk pengetahuan, `records/` dan `labels/` untuk catatan dan koreksi Anda; tidak ada lagi yang ditambahkan ke root proyek), dan sambungan ke agen sekali jalan: server MCP shikii untuk Claude Code (`.mcp.json`), Cursor (`.cursor/mcp.json`), OpenCode (`opencode.json`) dan Codex (`~/.codex/config.toml`), sebuah bagian di `AGENTS.md`, dan aturan rinci (skill) di `~/.claude/skills/shikii`. Entri milik Anda sendiri tidak pernah disentuh (entri shikii yang sudah ada dibiarkan). Hanya folder skill yang diganti setiap kali, jadi jangan menyuntingnya dengan tangan. `wiki/` menerima pengetahuan bersama (cara bertanya untuk tiap jenis pertanyaan, pola kata yang pernah berhasil, kebiasaan tiap model); penyempurna kata-kata (`tune --propose`) membacanya, dan halaman yang Anda sunting tidak pernah ditimpa oleh `init` berikutnya. Tidak punya Go? Pakai Docker di bawah.

Entri-entri itu mengarah ke model lokal (`shikii up`, port 8080). Hanya memakai model layanan luar? Hapus `--local` dengan tangan dari masing-masing keempat berkas. Berkas konfigurasi untuk alat yang tidak Anda pakai juga dibuat; hapus atau tambahkan ke `.gitignore` (`shikii init` kedua tidak pernah menyentuh yang sudah ada). Commit `.shikii/decisions/`, `.shikii/wiki/` dan `.shikii/labels/` (spec Anda, pengetahuan dan koreksi Anda: aset yang tetap hidup saat model berganti). `.shikii/records/` (setiap keputusan) terus bertambah, jadi commit atau tidak terserah Anda (`shikii gc` membuang yang lama). Jika Codex sudah punya `mcp_servers`, tidak ada yang ditambahkan dan baris-baris yang perlu ditempel dicetak. Proyek yang hanya punya `CLAUDE.md` tidak mendapat `AGENTS.md` (OpenCode akan berhenti membaca `CLAUDE.md`).

Mengubah halaman jawaban itu sendiri (`web/`, Svelte) membutuhkan Node 24 (`make web`). Memakai shikii tidak: halaman yang sudah dibangun di-commit dan ditanam dalam binary.

## Jalan masuk tersingkat

- **Docker**: tidak ada Go, tidak ada model? `docker compose up` menjalankan model lokal (Kev-4B) dan shikii, dengan halaman jawaban di http://127.0.0.1:8095 ([detail](../../integrations/docker.md))
- **Keputusan siap pakai**: enam buah di `decisions/` repo ini (jenis permintaan, laporan bug duplikat, pertanyaan yang sama, …). Salin satu ke `.shikii/decisions/` Anda, atau mulai darinya dengan `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` ([daftar](../../decisions/README.md))
- **Anda sudah punya tabel keputusan lama**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` mengubahnya menjadi koreksi, jadi Anda tidak perlu menjawab 50 yang pertama dengan tangan
- **Dari program Anda sendiri**: jalankan `shikii serve` dan panggil `POST /v1/decide` (memutuskan) dan `POST /v1/label` (mengoreksi). Isinya JSON dan `-H 'Content-Type: application/json'` wajib ada (semua endpoint: [panduan panjang](../guide.md#or-run-one-process-for-programs-and-people))
- **Contoh dari orang lain**: [menyalurkan surel pertanyaan](../examples/inquiry-route.md) (bahasa Jepang). Seorang pengguna baru menyelesaikan seluruh putaran hanya dengan README; spec dan 22 masukannya disertakan
- **Mengukur pemikiran AI sendiri** (Claude Code dan Codex): `shikii hook install` menambahkan dua hook yang menjalankan shikii sebelum dan sesudah setiap giliran. Sebelum: apakah permintaan ini akhirnya akan mengubah berkas, dan apakah agen perlu menyepakati cara kerjanya dulu? Sesudah: apakah perubahannya masih dalam batas yang Anda minta, dan apakah yang Anda minta sudah selesai? Kebenaran pertama didapat gratis dari apakah working tree berubah; sisanya Anda jawab di sebuah halaman. Tidak ada yang berubah sampai ada garis. Setelah itu permintaan yang perlu rencana menjadi catatan untuk agen, dan giliran yang tampak belum selesai atau di luar batas menjadi satu baris untuk Anda, dengan berapa kebenaran yang menopang garis itu dan presisi hasil pemeriksaan manusia. Tidak ada hook yang pernah menghentikan alat ([detail](../../integrations/claude-code.md), bahasa Jepang)
- **Di CI**: `uses: hayaenfj/shikii@main` menjalankan `shikii check` pada setiap pull request, sehingga kata-kata yang berubah atau garis yang melemah menggagalkan build

## Satu putaran penuh

```sh
shikii up                                                             # jalankan model lokal (diunduh saat pertama kali)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (belum ada garis, jadi: tanya orang)
shikii serve --local http://127.0.0.1:8080                                   # spec dan pengetahuan dibaca dari .shikii/
#   → jawab di http://127.0.0.1:8095. Begitu sebuah keputusan mencapai jumlah yang dimintanya (eval.min_n, 20 untuk yang ini) garis ditarik sendiri
#     Untuk mencoba dengan jawaban lebih sedikit, turunkan eval.min_n di spec ke jumlah yang Anda punya (garis yang ditulis dengan --force ditandai oleh check)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (aman untuk dijalankan, dengan buktinya: garis bertumpu pada 61 koreksi, 48 di antaranya dari manusia. Dari jawaban manusia yang oleh garis ini ditempatkan di sisi act, 25 buah, 96% benar)
```

Hanya ada empat band. `act` = jalankan, `ask` = tanya orang, `discard` = jangan lakukan apa pun, `abstain` = masukan kurang. Anda tidak pernah membaca probabilitas mentah.

## Apa yang baru

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="arsitektur shikii: tiga hal yang dilakukan manusia, aset (pertanyaan, kebenaran, tanda terima), putaran yang ditumbuhkan mesin, wiki yang bisa diverifikasi, model sekali pakai" width="100%"></p>

- **Pengetahuan terkumpul di pertanyaan dan koreksi, bukan di model.** Jadi model bisa dibuang, dan menggantinya tidak perlu kerja manusia. Terukur: dengan mesin memilih kata-kata dan tata letak serta menambahkan contoh serupa, model dasar biasa menyamai akurasi fine-tuning (LoRA) ([angka](../bench.md))
- **Keputusan otomatis datang dengan bukti.** Bukan "95% yakin" melainkan "presisi 0,96 pada 48 jawaban yang diperiksa manusia"
- **Wiki tidak dibaca, melainkan diukur.** "Bertanya dengan cara ini berhasil" terkumpul sebagai klaim dengan angka dan diuji ulang pada model baru. Hanya manusia yang memutuskan sebuah klaim terkonfirmasi

## Baca lebih lanjut

- [Panduan panjang](../guide.md): setiap perintah, mulai cepat lengkap, HTTP API, apa yang bisa Anda sambungkan, hasil ukur dan FAQ · [referensi spec](../../skills/shikii/references/spec.md)
- [Catatan desain](../decision-ops-framework.md): apa yang dimiliki kerangka ini dan apa yang ia tolak untuk dimiliki, serta apa yang sudah diverifikasi · [Arsitektur](../architecture.md) (bahasa Jepang)
- [Keputusan siap pakai](../../decisions/README.md) · [wiki cara bertanya](../../wiki/index.md) · [tolok ukur](../bench.md) · [perbandingan model](../model-comparison.md) · [log operasi](../operations.md)
- [Sambungan](../../integrations/mcp.md): MCP, adaptor yang mengubah model chat biasa menjadi model keputusan, Docker, Claude Code
- [Panduan bahasa Jepang](../guide.ja.md) dengan satu putaran penuh dari dunia nyata

## Berkontribusi

Issue dan pull request disambut baik. Tes yang menjaga suatu perilaku ditunjukkan gagal dulu: rusak perilakunya dengan sengaja, jalankan tesnya, catat bagaimana ia gagal, lalu perbaiki dan catat bahwa ia lulus. Setiap angka dalam dokumen berasal dari skrip. Hanya manusia yang mengubah status klaim wiki (`confirmed` / `tentative` / `rejected`), dengan run yang menjadi buktinya. Tidak ada opsi baru: tata letaknya (`decisions/` `wiki/` `.shikii/` di root proyek) dan aturan tetap yang bekerja.

## Lisensi

[Apache 2.0](../../LICENSE). Lisensi model terpisah: Kev-4B dan keluarga Bonsai berlisensi Apache 2.0, OpenJev CC BY-NC 4.0, Jev yang dihosting adalah layanan TypeSafe. shikii menyimpan probabilitas dan label Anda, bukan apa pun dari model.
