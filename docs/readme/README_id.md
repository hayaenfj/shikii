[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · **Bahasa Indonesia**

# shikii: Mengoperasikan Model Keputusan

### Satu siklus hidup untuk setiap model keputusan: define → decide → record → label → calibrate → check → drift.

**Model memberi Anda probabilitas. shikii memberi tahu apakah Anda boleh bertindak berdasarkan probabilitas itu.**

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-179%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[Catatan desain](../decision-ops-framework.md) · [Operasi](../operations.md) · [Perbandingan model](../model-comparison.md) · [Panduan lengkap (bahasa Jepang)](../guide.ja.md)**

[Mulai cepat](#mulai-cepat) · [Cara kerja shikii](#cara-kerja-shikii) · [Apa yang bisa dihubungkan](#apa-yang-bisa-dihubungkan) · [Hasil](#hasil) · [FAQ](#faq) · [Dokumentasi](#dokumentasi)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii tidak pernah menjalankan model dan tidak pernah membaca keychain. Ia hanya berbicara dengan endpoint model keputusan yang sudah Anda miliki, menyimpan catatan, dan menolak **act** sampai probabilitas untuk *pertanyaan ini pada model ini* terverifikasi dengan label. Blok kalibrasi dihasilkan otomatis, tidak pernah ditulis tangan.

---

Model keputusan (TypeSafe Jev, llama.cpp Kev-4B dan OpenJev, OpenRouter Decisions, …) menjawab pertanyaan bertipe, `choice`, `score`, dan `noul` (ya/tidak), dengan probabilitas dan tanpa teks yang dihasilkan. Setiap vendor menyertakan penafian yang sama: *probabilitas tidak dikalibrasi untuk data Anda, dan ambang batas tidak berlaku lintas model maupun lintas pertanyaan.*

shikii memegang bagian yang tidak disediakan siapa pun: **siklus hidup** sebuah keputusan. Sebuah file spec berisi pertanyaan, opsi, templat state, dan ambang batas. `decide` bertanya ke model dan mengembalikan sebuah band (`act` / `ask` / `discard` / `abstain`), tidak pernah angka mentah. `label` mencatat apa yang benar. `calibrate` melakukan fitting dan validasi silang, lalu menulis hasilnya kembali ke spec di bawah kunci yang terdiri dari hash pertanyaan dan nama model. `check` menggagalkan CI ketika redaksi atau model bergeser. `gc` menjaga store tetap kecil.

Ia juga disertai `shikii-readout`, adapter 500 baris yang mengubah **GGUF chat apa pun di llama.cpp standar** menjadi model keputusan dengan membaca logit dari huruf-huruf opsi. Dalam pengukuran kami, model ternary 27B biasa yang dibaca dengan cara ini menyamai akurasi model keputusan terlatih, dengan kecepatan 0.6 detik per pertanyaan di laptop.

> Namanya berasal dari bahasa Jepang: *shikii* (閾) berarti ambang batas, garis yang mengubah probabilitas menjadi tindakan. CLI-nya bernama `shikii`.

## Mulai cepat

Go 1.27 atau lebih baru. Tidak ada yang lain yang dibutuhkan untuk menjalankan pertama kali.

### Jalankan seluruh siklus tanpa key atau GPU

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # model palsu: decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # model palsu yang probabilitasnya perlu dikalibrasi; lihat calibrate memilih platt
```

### Arahkan ke model sungguhan

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) model keputusan lokal di llama.cpp (mengunduh Kev-4B, sekitar 2.5 GB, saat pertama dijalankan)
make server                                       # llama-server di :8080
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) TypeSafe Jev hosted. Key hanya diambil dari environment
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

Jawabannya berupa band, trace id, dan probabilitas. Sampai spec memuat kalibrasi untuk model ini, band paling tinggi hanya `ask`.

### Tutup siklusnya

```sh
bin/shikii label <trace_id> change_request          # or:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <model name from the response> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1 ketika redaksi berubah atau precision@act turun di bawah act
```

### Pakai GGUF apa pun sebagai model keputusan

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # llama-server standar dengan flash attention dan KV 8-bit, :8086
make readout                                               # shikii-readout di depannya, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Instal dan perbarui

```sh
make install          # menyalin bin/shikii ke ~/.local/bin/shikii
git pull && make install
```

## Cara kerja shikii

Setiap keputusan adalah satu file YAML. Isinya pertanyaan, opsi, templat state, tujuan yang boleh menerima state, ambang batas, dan blok kalibrasi yang dihasilkan oleh `calibrate --write`.

```yaml
id: prompt-intent
type: choice
question: What is the user asking the assistant to do?
options:
  - id: change_request
    description: Change something in the repository - edit, fix, add, remove, rename...
  - id: question
    description: Explain, investigate, compare or give an opinion, without changing anything.
state:
  template: "{{ message }}"
  required: [message]            # input hilang → abstain; model tidak pernah dipanggil
policy:
  send_to: [hosted, local]       # sesi yang tainted otomatis melewati hosted
  pii: possible                  # catatan menyimpan hash input, tidak pernah teksnya
thresholds:
  act: 0.75                      # ≥ act → act otomatis, hanya jika sudah dikalibrasi
  ask: 0.60                      # ≥ ask → tanya manusia; di bawahnya → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**Kunci kalibrasi adalah `(hash dari pertanyaan + templat + opsi, model id seperti yang dikembalikan endpoint)`.** Ubah satu kata pada pertanyaan, atau biarkan `jev-latest` berganti ke versi baru, dan kalibrasinya begitu saja hilang: band turun ke `ask` alih-alih diam-diam bertindak dengan konstanta yang sudah usang. Tata letak prompt juga bagian dari kunci: `shikii-readout` melaporkan tata letaknya dalam nama model (`…+state-last`), karena memindahkan state ke dalam prompt saja mengubah akurasi model 27B sebesar 5 poin dengan bobot yang sama.

`calibrate` membandingkan tiga metode dengan validasi silang 5-fold dan menulis pemenangnya. `identity` berarti probabilitas mentah sudah memenuhi precision@act dan model hanya ditandai sebagai terverifikasi. `platt` (σ((Δ−bias)/T)) untuk pertanyaan ya/tidak. `vector` (softmax((log p + b)/T)) untuk choice dan score, dan memperbaiki kebiasaan per opsi seperti "lari ke *other* saat ragu". Label parsial (`!option`, "bukan *yang itu*") dihitung untuk precision tetapi tidak untuk fitting. Label yang diperoleh dari file disimpan secara permanen, sehingga setelahnya `check` dan `drift` berjalan offline.

Setiap cabang yang menyangkut keamanan (perutean taint, tidak ada act tanpa kalibrasi, pembatalan hash usang, validasi label, deteksi inversi, pemilihan metode berbasis CV, `gc` dry-run, …) punya satu mutation tercatat yang membuat test gagal sebelum perbaikannya diterima. Ada 63 test Go di 8 package; demo berjalan di CI.

## Apa yang bisa dihubungkan

shikii berbicara dengan bentuk request System One (`POST /v1/systemone`), jadi apa pun yang melayaninya bisa dipakai. Adapter menangani sisanya.

| Target | Yang diterima shikii | Persyaratan dan panduan |
|---|---|---|
| **TypeSafe Jev** (hosted) | `choice` / `score` / `noul` dengan probabilitas, model id seperti `jev-1.13.0` | `TYPESAFE_API_KEY` di environment. Default untuk `--hosted` |
| **Model keputusan llama.cpp** (Kev-4B, OpenJev, Clef, …) | Bentuk yang sama, secara lokal, model id dari `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M` (`make server`). llama.cpp 0.6.0 atau lebih baru |
| **OpenRouter Decisions API** | Bentuk yang sama melalui OpenRouter | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **GGUF chat apa pun** lewat `shikii-readout` | Pembacaan logit huruf dalam bentuk System One, tanpa kalibrasi bawaan | `llama-server` standar 0.7.0 di belakangnya. [`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai** lewat [BonJev](https://github.com/ChezRD/bonjev) | Sama, dengan prompt sendiri dan kalibrasi bawaan BonJev | Build Rust dan fork Prism. [`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **Model palsu** (`bin/fake-systemone`) | Probabilitas deterministik untuk demo dan test | Tidak perlu apa-apa. `make fake` |
| **Hook Claude Code** (claude-jev) | Setiap prompt diklasifikasi, setiap giliran diberi label dari working tree | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

Taint adalah input kelas satu: berikan `--tainted` (atau biarkan hook Anda yang melakukannya) pada sesi yang menyentuh data produksi, dan request tidak akan pernah meninggalkan mesin. Jika tidak ada endpoint lokal yang dikonfigurasi, keputusan akan abstain.

## Hasil

Spec yang sama, label yang sama, empat set evaluasi (intent prompt dalam bahasa Inggris dan Jepang, 62 request; pencocokan kasus yang sama pada 32 pasang bahasa Jepang). Detail dan tabel lengkap ada di [`docs/model-comparison.md`](../model-comparison.md).

| Model | Lisensi | top-1 (semua / ja) | Brier ya/tidak (mentah) | Kalibrasi yang dipilih | Detik per pertanyaan |
|---|---|---|---|---|---|
| Kev-4B (model keputusan terlatih) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B (terlatih) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B lewat BonJev | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B lewat `shikii-readout`, llama.cpp standar** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**Redaksi mengalahkan kalibrasi.** Pada intent prompt, 19 dari 21 kesalahan jatuh ke *other*; menulis ulang opsi menaikkan top-1 dari 0.68 ke 0.90 sebelum ada kalibrasi apa pun. Kalibrasi untuk kasus "urutan benar, keyakinan kurang". Confusion matrix yang dicetak `calibrate` memberi tahu Anda sedang menghadapi kasus yang mana.

**Model yang sudah terkalibrasi tidak butuh Platt.** Ya/tidak milik Kev-4B punya Brier 0.046 saat mentah; Platt malah memperburuknya di bawah validasi silang, sehingga `identity` yang ditulis. Pertama kalinya `calibrate` memilih `platt` adalah pada LLM biasa yang dibaca lewat logit. Perlu tidaknya kalibrasi adalah sifat dari *model ditambah cara membacanya*, bukan sifat model saja.

**GGUF biasa mendarat tepat di samping model keputusan terlatih.** Model chat ternary 27B, llama.cpp mainline 0.7.0, satu adapter, tanpa fork, tanpa pelatihan, tanpa Rust: top-1 sama dengan model terlatih berlisensi Apache terbaik, bahasa Jepang 1.000, 0.58 detik per pertanyaan dengan flash attention dan prefix yang sudah dipanaskan. Memindahkan state dari sebelum opsi ke tepat sebelum jawaban menaikkannya dari 0.919 ke 0.968 dengan bobot yang sama, itulah sebabnya tata letak menjadi bagian dari kunci kalibrasi.

## FAQ

**Apakah shikii menjalankan model?** Tidak. Ia mengirim request System One ke endpoint yang Anda jalankan atau sewa, dan menyimpan semua yang terjadi setelah jawaban. `shikii-readout` adalah satu-satunya pengecualian secara semangat: ia mengubah logit model chat menjadi bentuk yang sama, tetapi inferensinya tetap milik llama.cpp.

**Mengapa keputusan saya tidak pernah mencapai `act`?** Karena spec tidak punya blok kalibrasi untuk `(hash pertanyaan ini, model id ini)`. Kumpulkan label (`label`, atau `--labeled file`), jalankan `calibrate --write`, lalu `status` menampilkan `calibrated: identity|platt|vector`. Keputusan tanpa precision terverifikasi pada ambang act tidak diizinkan untuk act. Itulah intinya.

**Saya mengubah satu kata pada pertanyaan dan kalibrasinya hilang.** Benar. Hash berubah, konstanta tadi di-fit untuk pertanyaan yang berbeda. `status` menampilkan label lama sebagai `stale hash`; jalankan ulang `calibrate --write` setelah Anda punya label untuk redaksi yang baru.

**Model mana yang sebaiknya saya pakai?** Tergantung lisensi, kecepatan, dan bahasa, dan shikii tidak memutuskan untuk Anda. Tabel di atas dan [`docs/model-comparison.md`](../model-comparison.md) adalah argumennya: dengan spec dan label yang sama Anda bisa memilih berdasarkan angka, termasuk apakah suatu model butuh kalibrasi sama sekali. `scripts/eval-model.sh <url> <name>` mereproduksi tabel untuk endpoint baru.

**Apakah berfungsi dalam bahasa Jepang?** Ya, dan di situlah perbedaannya terlihat. Kev-4B mendapat 0.913 pada prompt bahasa Jepang; model 27B mendapat 1.000. Pencocokan kasus yang sama pada ringkasan investigasi berbahasa Jepang memisahkan sampel positif dari negatif dengan sempurna pada setiap model 27B.

**Ke mana data saya pergi?** Hanya state yang sudah dirender yang dikirim ke endpoint yang disebut di `send_to`. Dengan `pii: possible` (default), store menyimpan hash input, tidak pernah teksnya. `--tainted` menjaga semuanya tetap lokal. Key dibaca dari `DECISIONS_API_KEY` atau `TYPESAFE_API_KEY` dan tidak dari tempat lain.

**Store terus membesar.** `shikii gc` menghapus catatan yang lebih tua dari 30 hari dan tidak pernah diberi label. Catatan berlabel dan label itu sendiri disimpan selamanya, karena itulah bahan kalibrasi. Perintah ini hanya dry run kecuali Anda menyertakan `--apply`.

**Apakah ia condong ke opsi pertama?** Ukur sebelum mengoreksi: `shikii bias <spec> --permutations 100` mengacak opsi dan melaporkan jumlah flip serta pergeseran posisi. Kev-4B: 0 flip dari 100, pergeseran ≤ 0.024. LLM biasa yang dibaca lewat logit mungkin berbeda; perintah ini ada untuk mereka.

## Dokumentasi

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): catatan desain, apa yang dimiliki framework dan apa yang sengaja tidak dimilikinya, rencana verifikasi, dan apa yang sudah terkonfirmasi.
- [`docs/operations.md`](../operations.md): cara menghentikan atau me-rollback setiap bagian yang bergerak, cara membaca kegagalan, apa yang masih belum ada.
- [`docs/model-comparison.md`](../model-comparison.md): tabel perbandingan dan cara mereproduksinya.
- [`docs/guide.ja.md`](../guide.ja.md): panduan panjang berbahasa Jepang dengan semua perintah dan satu siklus penuh di dunia nyata.
- [`integrations/readout.md`](../../integrations/readout.md), [`integrations/bonjev.md`](../../integrations/bonjev.md), [`integrations/claude-code.md`](../../integrations/claude-code.md): tiga koneksi yang kami jalankan.
- [`examples/`](../../examples/) dan [`testdata/`](../../testdata/README.md): spec tanpa kalibrasi sebagai titik awal, dan set berlabel sintetis yang dipakai dalam test.

Perintah-perintahnya:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: stale hash, blok tulisan tangan, precision@act di bawah act → exit 1
shikii drift     <spec> --model M [--window N]            # label terbaru dibandingkan dengan Brier dan precision yang tercatat saat kalibrasi
shikii bias      <spec> [--rows K] [--permutations N]     # apakah jawaban bergantung pada urutan opsi?
shikii status    [spec...] [--store DIR]                  # inventaris: catatan, label, sumber, status per (model, hash)
shikii gc        [spec...] [--older-than 30d] [--apply]   # hapus catatan lama tanpa label; default-nya dry run
shikii hash      <spec>
```

## Kontribusi

Issue dan pull request disambut. Satu aturan: test yang menjaga sebuah perilaku harus diperlihatkan gagal terlebih dahulu. Rusak perilaku itu dengan sengaja, jalankan test, catat bagaimana ia gagal, lalu perbaiki dan catat bahwa ia lulus. Dokumen mencantumkan mutation itu untuk setiap cabang keamanan; jaga agar daftarnya tetap jujur.

## Lisensi

[Apache 2.0](../../LICENSE). Lisensi model terpisah: Kev-4B dan keluarga Bonsai berlisensi Apache 2.0, OpenJev berlisensi CC BY-NC 4.0, Jev hosted adalah layanan TypeSafe. shikii menyimpan probabilitas dan label Anda, bukan apa pun milik model.
