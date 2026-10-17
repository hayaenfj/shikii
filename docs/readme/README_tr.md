[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · **Türkçe** · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

# shikii: karar modellerini işletin

### Her karar modeli için tek bir yaşam döngüsü: define → decide → record → label → calibrate → check → drift.

**Model size bir olasılık verir. shikii, bu olasılığa dayanarak harekete geçip geçemeyeceğinizi söyler.**

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/github/actions/workflow/status/hayaenfj/shikii/test.yml?style=flat-square&label=tests)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[Tasarım notları](../decision-ops-framework.md) · [İşletim](../operations.md) · [Model karşılaştırması](../model-comparison.md)**

[Hızlı başlangıç](#hızlı-başlangıç) · [shikii nasıl çalışır](#shikii-nasıl-çalışır) · [Neleri bağlayabilirsiniz](#neleri-bağlayabilirsiniz) · [Sonuçlar](#sonuçlar) · [FAQ](#faq) · [Dokümantasyon](#dokümantasyon)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii hiçbir zaman model çalıştırmaz ve hiçbir zaman keychain okumaz. Zaten elinizde olan bir karar modeli uç noktasıyla konuşur, kayıtları tutar ve *bu soru için bu modelin* olasılıkları etiketlerle doğrulanana kadar **act** vermeyi reddeder. Kalibrasyon blokları üretilir, elle yazılmaz.

---

Karar modelleri (TypeSafe Jev, llama.cpp üzerinde Kev-4B ve OpenJev, OpenRouter Decisions, …) tipli sorulara, yani `choice`, `score` ve `noul` (evet/hayır) sorularına, metin üretmeden yalnızca olasılıklarla yanıt verir. Her sağlayıcı aynı uyarıyı ekler: *olasılıklar sizin verinize göre kalibre edilmemiştir ve eşikler modeller ya da sorular arasında taşınmaz.*

shikii kimsenin sunmadığı kısmı üstlenir: bir kararın **yaşam döngüsü**. Bir spec dosyası soruyu, seçenekleri, durum şablonunu ve eşikleri tutar. `decide` bir modele sorar ve ham bir sayı değil, bir bant (`act` / `ask` / `discard` / `abstain`) döndürür. `label` neyin doğru olduğunu kaydeder. `calibrate` uydurur ve çapraz doğrular, ardından sonucu sorunun hash'i ile modelin adından oluşan bir anahtarla spec'e geri yazar. `check`, ifade ya da model kaydığında CI'ı düşürür. `gc` store'u küçük tutar.

Ayrıca `shikii-readout` ile gelir: seçenek harflerinin logit'lerini okuyarak **sade llama.cpp üzerindeki herhangi bir sohbet GGUF'unu** karar modeline çeviren 500 satırlık bir adaptör. Ölçümlerimizde bu şekilde okunan sıradan bir 27B üçlü (ternary) model, doğrulukta eğitilmiş karar modelleriyle eşleşti; bir dizüstü bilgisayarda soru başına 0,6 saniyeyle.

> Ad Japonca: *shikii* (閾) Japonca'da eşik demektir, yani olasılığı eyleme çeviren çizgi. CLI'ın adı `shikii`.

## Hızlı başlangıç

Go 1.24 veya üstü. İlk çalıştırma için başka hiçbir şey gerekmez.

### Tüm döngüyü anahtar ve GPU olmadan çalıştırın

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # sahte model: decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # olasılıkları kalibrasyon gerektiren sahte bir model; calibrate'in platt seçişini izleyin
```

### Gerçek bir modele yönlendirin

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) llama.cpp üzerinde yerel karar modeli (ilk çalıştırmada Kev-4B'yi, yaklaşık 2,5 GB, indirir)
make server                                       # :8080 üzerinde llama-server
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) barındırılan TypeSafe Jev. Anahtar yalnızca ortam değişkeninden gelir
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

Yanıt bir bant, bir trace id ve olasılıklardır. Spec bu model için bir kalibrasyon taşımadıkça bant en fazla `ask` olur.

### Döngüyü kapatın

```sh
bin/shikii label <trace_id> change_request          # veya:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <yanıttaki model adı> --write
bin/shikii check examples/prompt-intent.yaml        # ifade değiştiyse veya precision@act, act'in altına düştüyse exit 1
```

### Herhangi bir GGUF'u karar modeli olarak kullanın

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # flash attention ve 8-bit KV ile sade llama-server, :8086
make readout                                               # önünde shikii-readout, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Kurulum ve güncelleme

```sh
make install          # bin/shikii dosyasını ~/.local/bin/shikii konumuna kopyalar
git pull && make install
```

## shikii nasıl çalışır

Her karar tek bir YAML dosyasıdır. Soru, seçenekler, durum şablonu, durumun nereye gönderilebileceği, eşikler ve `calibrate --write` tarafından üretilen kalibrasyon blokları.

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
  required: [message]            # girdi eksikse → abstain; model hiç çağrılmaz
policy:
  send_to: [hosted, local]       # taint'li oturum hosted'ı otomatik atlar
  pii: possible                  # kayıtlar girdilerin hash'ini tutar, metni asla
thresholds:
  act: 0.75                      # ≥ act → otomatik harekete geç, yalnızca kalibre edilmişse
  ask: 0.60                      # ≥ ask → bir insana sor; altında → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**Kalibrasyon anahtarı `(soru + şablon + seçeneklerin hash'i, uç noktanın döndürdüğü model id'si)` çiftidir.** Sorunun tek bir sözcüğünü değiştirirseniz ya da `jev-latest` yeni bir sürüme kayarsa kalibrasyon basitçe kaybolur: bant, eskimiş sabitlerle sessizce harekete geçmek yerine `ask`'a düşer. Prompt yerleşimi de anahtarın parçasıdır: `shikii-readout` yerleşimini model adında bildirir (`…+state-last`), çünkü durumu prompt içinde taşımak, aynı ağırlıklarla bir 27B modelin doğruluğunu 5 puan değiştirdi.

`calibrate` üç yöntemi 5 katlı çapraz doğrulamayla karşılaştırır ve kazananı yazar. `identity`, ham olasılıkların zaten precision@act'i karşıladığı ve modelin yalnızca doğrulanmış olarak işaretlendiği anlamına gelir. `platt` (σ((Δ−bias)/T)) evet/hayır soruları içindir. `vector` (softmax((log p + b)/T)) choice ve score içindir ve seçenek başına alışkanlıkları, örneğin "*other*'a sığınma"yı düzeltir. Kısmi etiketler (`!option`, "bu *değildi*") precision'a sayılır ama uydurmaya katılmaz. Dosyadan alınan etiketler kalıcı olarak saklanır, bu yüzden `check` ve `drift` sonrasında çevrimdışı çalışır.

Güvenlikle ilgili her dal (taint yönlendirmesi, kalibrasyonsuz act yok, eski hash'in geçersizleşmesi, etiket doğrulama, ters çevrilme tespiti, CV ile yöntem seçimi, dry-run `gc`, …) için, düzeltme kabul edilmeden önce bir testi düşüren kayıtlı bir mutasyon vardır. 8 pakette 63 Go testi var; demo CI'da çalışır.

## Neleri bağlayabilirsiniz

shikii System One istek biçimini (`POST /v1/systemone`) konuşur, bu yüzden onu sunan her şey çalışır. Gerisini adaptör kapsar.

| Hedef | shikii'nin aldığı | Gereksinimler ve kılavuz |
|---|---|---|
| **TypeSafe Jev** (barındırılan) | Olasılıklarıyla `choice` / `score` / `noul`, `jev-1.13.0` gibi model id'si | Ortamda `TYPESAFE_API_KEY`. Varsayılan `--hosted` |
| **llama.cpp karar modelleri** (Kev-4B, OpenJev, Clef, …) | Aynı biçim, yerelde, model id'si `/v1/models`'tan | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M` (`make server`). llama.cpp 0.6.0 veya üstü |
| **OpenRouter Decisions API** | OpenRouter üzerinden aynı biçim | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| `shikii-readout` ile **herhangi bir sohbet GGUF'u** | System One biçiminde harf-logit okuması, kendi kalibrasyonu yok | Arkasında sade `llama-server` 0.7.0. [`integrations/readout.md`](../../integrations/readout.md) |
| [BonJev](https://github.com/ChezRD/bonjev) ile **PrismML Bonsai** | Aynısı, BonJev'in kendi prompt'u ve yerleşik kalibrasyonuyla | Rust derlemesi ve Prism fork'u. [`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **Sahte model** (`bin/fake-systemone`) | Demolar ve testler için deterministik olasılıklar | Hiçbir şey. `make fake` |
| **Claude Code hook'u** (claude-jev) | Her prompt sınıflandırılır, her tur çalışma ağacından etiketlenir | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

Taint birinci sınıf bir girdidir: üretim verisine dokunmuş bir oturumda `--tainted` verin (ya da hook'unuza verdirin), istek makineden asla çıkmaz. Yerel uç nokta yapılandırılmamışsa karar abstain eder.

## Sonuçlar

Aynı spec'ler, aynı etiketler, dört değerlendirme kümesi (İngilizce ve Japonca prompt niyeti, 62 istek; 32 Japonca çiftte aynı vaka eşleştirmesi). Ayrıntılar ve tam tablo [`docs/model-comparison.md`](../model-comparison.md) içinde.

| Model | Lisans | top-1 (tümü / ja) | Evet/hayır Brier (ham) | Seçilen kalibrasyon | Soru başına saniye |
|---|---|---|---|---|---|
| Kev-4B (eğitilmiş karar modeli) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B (eğitilmiş) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| BonJev ile Bonsai 2 27B | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **`shikii-readout` ile Ternary-Bonsai-27B, sade llama.cpp** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**İfade, kalibrasyonu yener.** Prompt niyetinde 21 hatanın 19'u *other*'a düştü; seçeneklerin yeniden ifade edilmesi, herhangi bir kalibrasyondan önce top-1'i 0,68'den 0,90'a çıkardı. Kalibrasyon "sıralama doğru, güven yetersiz" durumları içindir. `calibrate`'in bastığı karışıklık matrisi hangi durumda olduğunuzu söyler.

**Zaten kalibre modellerin Platt'a ihtiyacı yoktur.** Kev-4B'nin evet/hayır sorusunda ham Brier 0,046'ydı; Platt çapraz doğrulamada durumu kötüleştirdi, bu yüzden `identity` yazıldı. `calibrate`'in `platt`'ı ilk seçişi, logit'lerle okunan sıradan bir LLM'de oldu. Kalibrasyonun gerekip gerekmediği *modelin ve okumanın birlikte* bir özelliğidir, tek başına modelin değil.

**Sıradan bir GGUF, eğitilmiş bir karar modelinin yanına konar.** 27B üçlü bir sohbet modeli, ana hat llama.cpp 0.7.0, tek bir adaptör; fork yok, eğitim yok, Rust yok: Apache lisanslı en iyi eğitilmiş modelle aynı top-1, Japoncada 1.000, flash attention ve ısıtılmış önekle soru başına 0,58 sn. Durumu seçeneklerin öncesinden yanıtın hemen öncesine taşımak, aynı ağırlıklarla sonucu 0,919'dan 0,968'e çıkardı; yerleşimin kalibrasyon anahtarının parçası olmasının nedeni budur.

## FAQ

**shikii model çalıştırır mı?** Hayır. System One isteklerini sizin işlettiğiniz ya da kiraladığınız bir uç noktaya gönderir ve yanıttan sonra olan her şeyi tutar. `shikii-readout` ruh olarak tek istisnadır: bir sohbet modelinin logit'lerini aynı biçime çevirir, ancak çıkarım yine llama.cpp'nindir.

**Kararım neden hiç `act`'e ulaşmıyor?** Çünkü spec'te `(bu sorunun hash'i, bu model id'si)` için bir kalibrasyon bloğu yok. Etiket toplayın (`label` ya da `--labeled file`), `calibrate --write` çalıştırın; `status` `calibrated: identity|platt|vector` gösterir. Act eşiğinde doğrulanmış precision'ı olmayan bir karar harekete geçemez. Mesele de bu.

**Sorudaki tek bir sözcüğü değiştirdim ve kalibrasyon kayboldu.** Doğru. Hash değişti, sabitler farklı bir soruya uydurulmuştu. `status` eski etiketleri `stale hash` olarak gösterir; yeni ifade için etiketleriniz olunca `calibrate --write` komutunu yeniden çalıştırın.

**Hangi modeli kullanmalıyım?** Lisansa, hıza ve dile bağlıdır ve shikii sizin yerinize karar vermez. Yukarıdaki tablo ve [`docs/model-comparison.md`](../model-comparison.md) bunun gerekçesidir: aynı spec ve aynı etiketlerle, bir modelin kalibrasyona hiç ihtiyaç duyup duymadığı dahil, sayılara göre seçebilirsiniz. `scripts/eval-model.sh <url> <name>` yeni bir uç nokta için tabloyu yeniden üretir.

**Japoncada çalışır mı?** Evet, farkların göründüğü yer de orası. Kev-4B Japonca prompt'larda 0,913 alır; 27B modeller 1,000. Japonca soruşturma özetlerinde aynı vaka eşleştirmesi, her 27B modelde pozitifleri negatiflerden tamamen ayırır.

**Verilerim nereye gidiyor?** Yalnızca işlenmiş durum, `send_to`'da adı geçen uç noktaya gider. `pii: possible` (varsayılan) ile store girdilerin hash'ini tutar, metni asla. `--tainted` her şeyi yerelde tutar. Anahtarlar `DECISIONS_API_KEY` veya `TYPESAFE_API_KEY`'den okunur, başka hiçbir yerden değil.

**Store büyümeye devam ediyor.** `shikii gc`, hiç etiket almamış 30 günden eski kayıtları siler. Etiketli kayıtlar ve etiketler, kalibrasyon malzemesi oldukları için sonsuza dek kalır. `--apply` vermediğiniz sürece yalnızca dry run'dır.

**İlk seçeneğe doğru yanlılık var mı?** Düzeltmeden önce ölçün: `shikii bias <spec> --permutations 100` seçenekleri karıştırır ve dönmeleri ile konum kaymasını raporlar. Kev-4B: 100'de 0 dönme, kayma ≤ 0.024. Logit'lerle okunan sıradan LLM'ler farklı olabilir; komut onlar için var.

## Dokümantasyon

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): tasarım notları, framework'ün neye sahip olduğu ve sahip olmayı neyi reddettiği, doğrulama planı ve nelerin doğrulandığı.
- [`docs/operations.md`](../operations.md): her hareketli parçanın nasıl durdurulacağı veya geri alınacağı, bir arızanın nasıl okunacağı, hâlâ nelerin eksik olduğu.
- [`docs/model-comparison.md`](../model-comparison.md): karşılaştırma tablosu ve nasıl yeniden üretileceği.
- [`docs/guide.ja.md`](../guide.ja.md): her komutu ve gerçek hayattan eksiksiz bir döngüyü içeren uzun Japonca kılavuz.
- [`integrations/readout.md`](../../integrations/readout.md), [`integrations/bonjev.md`](../../integrations/bonjev.md), [`integrations/claude-code.md`](../../integrations/claude-code.md): işlettiğimiz üç bağlantı.
- [`examples/`](../../examples/) ve [`testdata/`](../../testdata/README.md): başlamak için kalibrasyonsuz bir spec ve testlerde kullanılan sentetik etiketli kümeler.

Komutlar:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: stale hash, elle yazılmış bloklar, precision@act act'in altında → exit 1
shikii drift     <spec> --model M [--window N]            # son etiketler, kalibrasyonda kaydedilen Brier ve precision'a karşı
shikii bias      <spec> [--rows K] [--permutations N]     # yanıt seçenek sırasına bağlı mı?
shikii status    [spec...] [--store DIR]                  # envanter: kayıtlar, etiketler, kaynaklar, (model, hash) başına durum
shikii gc        [spec...] [--older-than 30d] [--apply]   # eski etiketsiz kayıtları sil; varsayılan dry run
shikii hash      <spec>
```

## Katkıda bulunma

Issue'lar ve pull request'ler memnuniyetle karşılanır. Tek bir kural var: bir davranışı koruyan testin önce başarısız olduğu gösterilmelidir. Davranışı bilerek bozun, testi çalıştırın, nasıl başarısız olduğunu kaydedin, sonra düzeltin ve geçtiğini kaydedin. Dokümanlar bu mutasyonları her güvenlik dalı için listeler; bu listeyi dürüst tutun.

## Lisans

[Apache 2.0](../../LICENSE). Model lisansları ayrıdır: Kev-4B ve Bonsai ailesi Apache 2.0, OpenJev CC BY-NC 4.0, barındırılan Jev TypeSafe'in hizmetidir. shikii sizin olasılıklarınızı ve etiketlerinizi saklar, modellerden hiçbir şeyi değil.
