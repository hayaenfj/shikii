[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · **Bahasa Indonesia**

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: mengubah "mungkin" dari AI menjadi "aman untuk dijalankan" yang bisa Anda pakai menjalankan bisnis

### Alat yang membiarkan AI memutuskan "ya atau tidak", "yang mana" dan "seberapa banyak", lalu menumbuhkannya dari koreksi Anda. Yang Anda lakukan hanya menjawab, di sebuah halaman, apa yang sebenarnya benar.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-334%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../decision-ops-framework.md)

[Masalahnya](#masalahnya) · [Cara pakai 1: mengukur cara berpikir AI itu sendiri](#cara-pakai-1-mengukur-cara-berpikir-ai-itu-sendiri) · [Cara pakai 2: serahkan keputusan bisnis kepadanya](#cara-pakai-2-serahkan-keputusan-bisnis-kepadanya) · [Apa yang Anda lakukan di halaman](#apa-yang-anda-lakukan-di-halaman) · [Instalasi](#instalasi) · [Mengapa AI-nya boleh diganti](#mengapa-ai-nya-boleh-diganti) · [Baca lebih lanjut](#baca-lebih-lanjut)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## Masalahnya

Tanyakan pada AI "apakah pertanyaan ini perkara yang sama dengan yang sebelumnya?" dan ia menjawab "mungkin, 0,9". Untuk pekerjaan, itu belum cukup. Apakah 0,9 itu benar-benar tepat sembilan dari sepuluh kali? Apakah ia diam-diam menjadi keliru sejak hari Anda beralih ke AI yang lebih baru? Apakah keputusan yang ia ambil saat tidak ada yang mengawasi masih benar? Kalau seseorang harus memeriksa setiap kali, tidak banyak gunanya memakai AI.

shikii menarik sebuah **garis** dari koreksi Anda: di atas garis ini, AI boleh bertindak sendiri. Sebelum ada garis, tidak ada yang dipercaya. Setelah garis ada, setiap keputusan otomatis membawa dasarnya: garis itu bertumpu pada berapa koreksi, dan seberapa sering ia benar. Garis itu hidup bersama pertanyaan dan koreksinya, bukan di dalam AI, sehingga mengganti AI tidak memerlukan kerja manusia.

Ada dua cara memakainya: mengukur cara berpikir AI yang menulis kode Anda (yang paling ringan), dan menyerahkan kepadanya sebuah keputusan dari bisnis Anda.

## Cara pakai 1: mengukur cara berpikir AI itu sendiri

Untuk orang yang memakai Claude Code atau Codex. Agen pengode membuat penilaian saat menerima permintaan dan saat selesai; shikii mengukurnya tanpa mengganggu.

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

Itu saja. Perintah itu memasang binary, menambahkan dua hook ke Claude Code dan Codex, meletakkan aturan agen (sebuah skill), dan menjalankan AI lokal kecil (Kev-4B) di latar belakang (kalau llama.cpp belum ada, ia mencetak satu baris cara memasangnya). Buka terminal baru dan lanjutkan bekerja dengan agen seperti biasa (di Codex, jalankan `/hooks` sekali untuk mempercayai hook shikii). Empat hal diukur.

| Kapan | Pertanyaan | Dari mana kebenarannya |
|---|---|---|
| Permintaan masuk | Apakah permintaan ini akhirnya akan mengubah berkas? | Otomatis: apakah working tree berubah sebelum permintaan berikutnya |
| Permintaan masuk | Apakah agen perlu menyepakati cara kerjanya dengan Anda sebelum menyentuh apa pun? | Anda, di halaman |
| Giliran berakhir | Apakah perubahannya masih dalam batas yang Anda minta? | Anda, di halaman |
| Giliran berakhir | Apakah yang Anda minta sudah selesai? | Anda, di halaman |

Di akhir giliran muncul satu baris, `shikii: 3 keputusan dari sesi ini menunggu jawaban Anda: http://127.0.0.1:8096/`, dan Anda membukanya saat ada waktu sebentar (hook juga menjalankan halamannya di latar belakang); setiap jawaban hanya butuh beberapa detik. Setelah 50 jawaban, garis ditarik dan perilaku agen berubah: permintaan yang membutuhkan rencana menjadi catatan untuk agen, dan giliran yang tampak belum selesai atau di luar batas menjadi satu baris untuk Anda, masing-masing dengan dasar seperti "garis pada 61 kebenaran, presisi hasil pemeriksaan manusia 0,96". Sampai saat itu hook hanya mencatat. Tidak ada hook yang pernah menghentikan alat.

Semuanya berada di satu tempat, `~/.shikii/agent`, dengan satu garis per pertanyaan untuk semua proyek Anda (pertanyaannya tentang cara berpikir agen, jadi kebenarannya sama di setiap repositori). Setiap catatan ditandai dengan repositori asalnya, dan halaman bisa menyaring berdasarkan itu. Detail: [hook](../../integrations/claude-code.md) (bahasa Jepang).

## Cara pakai 2: serahkan keputusan bisnis kepadanya

Untuk keputusan yang Anda ulang terus di tempat kerja, seperti "apakah pertanyaan ini perkara yang sama?" atau "apakah pesan ini permintaan perubahan?": biarkan AI memutuskan, dan tumbuhkan dari koreksi Anda. Di sini pun tidak ada perintah yang perlu diketik. Anda cukup memberi tahu agen.

> Saya ingin shikii memutuskan apakah sebuah pertanyaan adalah perkara yang sama dengan yang sebelumnya, dan menumbuhkannya dari koreksi saya

Agen mengikuti aturannya (skill): ia membuat `.shikii/` di proyek (`shikii init`), menetapkan pertanyaan, pilihan dan masukannya lalu menulis berkas keputusan (`shikii new`; pertanyaan dan pilihannya ditunjukkan kepada Anda sekali, karena itulah maksud Anda), memanggil keputusan (`shikii decide`), menjalankan halaman jawaban (http://127.0.0.1:8095) dan memberi tahu Anda. Sebelum ada garis, tidak ada keputusan yang `act` (jalankan). Saat Anda menjawab di halaman, garis ditarik otomatis begitu jumlah yang ditetapkan keputusan itu sendiri tercapai (20 dalam contoh ini), dan sejak itu:

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (aman untuk dijalankan. Garis bertumpu pada 61 kebenaran, 48 di antaranya dijawab manusia; dari jawaban manusia yang oleh garis ini dibolehkan bertindak, 25 buah, 96% benar)
```

Hanya ada empat nilai `band`: `act` = jalankan, `ask` = tanya orang, `discard` = jangan lakukan apa pun, `abstain` = masukan kurang. Probabilitas mentah tidak pernah menjadi dasar Anda bertindak.

- **Dengan tangan**: `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`. Semuanya ada di [panduan](../guide.md)
- **Dari agen**: `shikii init` menulis pengaturan MCP, sehingga Claude Code, Cursor, OpenCode dan Codex bisa langsung memanggil alat `shikii_decide` dan `shikii_label` ([MCP](../../integrations/mcp.md))
- **Dari program Anda sendiri**: jalankan `shikii serve` dan panggil `POST /v1/decide` (memutuskan) dan `POST /v1/label` (mengoreksi). Isinya JSON dan butuh `-H 'Content-Type: application/json'` ([HTTP API](../guide.md#or-run-one-process-for-programs-and-people))
- **Keputusan siap pakai**: enam buah di `decisions/` repo ini (jenis permintaan, laporan bug duplikat, pertanyaan yang sama, …). Salin satu dengan `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` dan ubah kata-katanya ([daftar](../../decisions/README.md))
- **Anda sudah punya tabel keputusan lama**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` mengubahnya menjadi koreksi, jadi Anda tidak perlu menjawab 50 yang pertama dengan tangan
- **Docker**: belum ada Go dan belum ada model? `docker compose up` menjalankan AI lokal dan shikii, dengan halaman di http://127.0.0.1:8095 ([detail](../../integrations/docker.md))
- **Contoh dari orang lain**: [menyalurkan surel pertanyaan](../examples/inquiry-route.md) (bahasa Jepang). Seorang pengguna baru menyelesaikan seluruh putaran hanya dengan README; berkas keputusan dan 22 masukannya disertakan

## Apa yang Anda lakukan di halaman

Di halaman jawaban (`shikii serve`) seseorang melakukan tiga hal. Semua yang lain adalah pekerjaan mesin.

1. **Menjawab.** Halaman menampilkan keputusan yang AI tidak yakin, plus 1 dari 20 keputusan yang ia jalankan sendiri, dan Anda menekan tombol untuk apa yang sebenarnya benar. Tombol 1 sampai 9 dan Enter (sama dengan pilihan AI) membuatnya cepat. "Sudah dijawab" memungkinkan Anda memperbaiki jawaban sebelumnya (yang terbaru yang dihitung)
2. **Menulis cara bertanya lain.** Kalau Anda berpikir "bertanya dengan cara ini mungkin lebih berhasil", ketik di panel "Kata-kata" dengan kata-kata Anda sendiri. Mesin menilainya terhadap kata-kata saat ini pada koreksi yang sama (untuk keputusan agen itu sendiri, otomatis setiap 50 jawaban baru) dan hanya mengatakan "boleh diadopsi" kalau benar-benar lebih baik. Anda juga bisa membiarkan AI mengusulkan cara bertanya. Anda tidak pernah membuka berkas
3. **Menekan terima atau tolak.** Anda melihat apakah maknanya berubah, lalu menekan. Mesin memeriksa angkanya; Anda memeriksa maknanya

"Detail" menambahkan keadaan garis per model, "tanya AI lagi" (memutuskan ulang teks yang sama dengan kata-kata dan garis hari ini dan menampilkan sebelum dan sesudah), dan panel pengetahuan (klaim berbentuk "bertanya dengan cara ini berhasil", beserta buktinya). Catatan hanya ditambahkan, tidak pernah dihapus; keputusan baru menggantikan tempat yang lama.

## Instalasi

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 atau lebih baru; kalau tidak ada, Docker (di atas)
```

Yang dilakukan install.sh: memasang binary (`shikii` dan `shikii-readout`), menambahkan dua hook ke Claude Code dan Codex, meletakkan aturan agen (skill) di `~/.claude/skills/shikii`, dan, kalau llama.cpp ada, menjalankan AI lokal (Kev-4B di :8080) di latar belakang. Untuk memakai Jev yang dihosting atau OpenRouter, letakkan kuncinya di variabel lingkungan ([sambungan](../guide.md#what-you-can-connect)). `shikii doctor` memberi tahu apa yang berjalan dan apa yang kurang.

Penyiapan per proyek (`shikii init`) adalah pekerjaan agen (Anda juga boleh menjalankannya sendiri). Ia membuat satu folder, `.shikii/` (di dalamnya: `decisions/` = berkas keputusan, `wiki/` = pengetahuan, `records/` dan `labels/` = catatan dan koreksi). Tidak ada lagi yang muncul di root proyek. `decisions/`, `wiki/` dan `labels/` adalah aset yang tetap hidup saat AI berganti, jadi commit-lah; `records/` terus bertambah, jadi commit atau tidak terserah Anda (`shikii gc` membuang yang lama). Ia juga menambahkan entri shikii ke pengaturan agen Anda (`.mcp.json` Claude Code, `.cursor/mcp.json` Cursor, `opencode.json` OpenCode, `~/.codex/config.toml` Codex) tanpa menyentuh entri yang Anda tulis. Menjalankannya lagi tidak mengubah apa pun yang sudah ada.

Keluaran perintah dan pesan hook dalam bahasa Inggris (kalau pengaturan bahasa Claude Code adalah bahasa Jepang, pesan hook ditampilkan dalam bahasa Jepang di samping bahasa Inggrisnya). Halaman tersedia dalam bahasa Jepang. Hal-hal yang lebih rinci, seperti memakai AI yang dihosting saja, `AGENTS.md` versus `CLAUDE.md`, dan Node untuk mengubah halaman itu sendiri, ada di [panduan](../guide.md).

## Mengapa AI-nya boleh diganti

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="arsitektur shikii: tiga hal yang dilakukan manusia, aset (pertanyaan, kebenaran, tanda terima), putaran yang ditumbuhkan mesin, wiki yang bisa diverifikasi, model sekali pakai" width="100%"></p>

- **Pengetahuan terkumpul bersama pertanyaan dan koreksi, bukan di dalam AI.** Koreksi tetap ada walau AI berganti, sehingga AI baru bisa memakainya apa adanya dan garis ditarik ulang. Dalam pengukuran kami, membiarkan mesin memilih kata-kata dan tata letak serta menambahkan contoh serupa mencapai akurasi yang sama dengan fine-tuning bobot model (LoRA) ([angka](../bench.md))
- **Keputusan otomatis membawa tanda terima.** Bukan "95% yakin" melainkan "presisi 0,96 pada 48 kasus yang diperiksa manusia"
- **Wiki tidak dibaca, melainkan diukur.** "Bertanya dengan cara ini berhasil" terkumpul sebagai klaim dengan angka, dan bisa diuji ulang pada AI baru. Hanya manusia yang memutuskan sebuah klaim terkonfirmasi

## Baca lebih lanjut

- [Panduan](../guide.md): setiap perintah, HTTP API, FAQ · [referensi spec](../../skills/shikii/references/spec.md)
- [Hook](../../integrations/claude-code.md) (bahasa Jepang): cara mengukur pemikiran agen itu sendiri, tabel band → perilaku, isi dalamnya
- [Arsitektur](../architecture.md) (bahasa Jepang): gambaran utuh di balik diagram di atas
- [Keputusan siap pakai](../../decisions/README.md) · [wiki cara bertanya](../../wiki/index.md) · [angka hasil ukur](../bench.md)
- [Sambungan](../../integrations/mcp.md): MCP, adaptor untuk model chat biasa, Docker

**Untuk kontributor**: rusak sebuah tes dengan sengaja dan lihat ia gagal sebelum memperbaikinya. Setiap angka berasal dari skrip. Hanya manusia yang mengonfirmasi klaim wiki. Jangan tambahkan pengaturan; pakai tempat tetap dan aturan tetap.

## Lisensi

[Apache-2.0 OR MIT](../../LICENSE). Lisensi model terpisah (Kev-4B dan keluarga Bonsai berlisensi Apache 2.0, OpenJev CC BY-NC 4.0, Jev yang dihosting adalah layanan TypeSafe).
