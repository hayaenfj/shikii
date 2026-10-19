[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · **Türkçe** · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: yapay zekânın "muhtemelen" demesini, üzerine iş kurabileceğiniz bir "güvenle yapılabilir"e çevirin

### Bir yapay zekâya evet/hayır, hangisi ya da ne kadar sorusunu karar verdirmek ve bu kararı günlük işte güvenilir tutmak için bir araç. Siz bazı cevapları kontrol edersiniz ve ara sıra "hâlâ demek istediğim bu mu?" diye onaylarsınız. Gerisini makine yapar.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-190%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[Sorun](#sorun) · [shikii ne yapar](#shikii-ne-yapar) · [Siz ne yaparsınız](#siz-ne-yaparsınız) · [Kurulum](#kurulum) · [En kısa yollar](#en-kısa-yollar) · [Tam bir tur](#tam-bir-tur) · [Yeni olan ne](#yeni-olan-ne) · [Daha fazla okuma](#daha-fazla-okuma)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii: bir model size bir olasılık verir, shikii buna göre hareket edebilir misiniz söyler" width="100%"></p>

## Sorun

Bir yapay zekâya "bu başvuru öncekiyle aynı konu mu?" diye sorun, "muhtemelen, 0,9" der. Gerçek iş için bu yeterli değildir.

- Bu 0,9 gerçekten 10 seferin 9'unda doğru mu?
- Yeni bir modele geçtiğiniz günden beri sessizce yanlış söylemeye başladı mı?
- Soruyu biraz farklı yazarsanız doğruluk nasıl değişir?
- Kimse bakmazken kendi başına verdiği kararlar hâlâ doğru mu?

Her cevabı bir insan kontrol edecekse, yapay zekâyı kullanmanın pek anlamı kalmaz.

## shikii ne yapar

1. **Düzeltmelerinizi toplar ve yapay zekânın kendi başına hareket edebileceği çizgiyi çizer.** Verdiğiniz cevaplardan ("aslında doğrusu buydu") yapay zekânın güveni ile ne sıklıkla haklı çıktığı arasındaki ilişkiyi ölçer ve nereden yukarısına güvenilebileceğine karar verir. Bir çizgi olmadan hiçbir şey kendi başına hareket etmez. Çizgi çizildikten sonra her otomatik karar, çizginin kaç kontrol edilmiş cevaba dayandığını ve bunların ne sıklıkla doğru olduğunu söyleyen bir makbuz taşır
2. **Başka soru biçimlerini dener ve iyi olanı tutar.** Aynı düzeltmeler üzerinde alternatif soruları, seçenek açıklamalarını ve istem düzenlerini puanlar; birini yalnızca gerçekten daha iyiyse benimser. Siz sadece anlamın değişmediğini onaylarsınız
3. **Modeli sıfırdan başlamadan değiştirmenizi sağlar.** Düzeltmeler modelden ayrı durur, bu yüzden yeni bir model onları yeniden kullanır ve çizgi makine tarafından yeniden çizilir. İnsan emeği gerekmez
4. **Öğrendiklerini bir wiki'de saklar.** "Böyle sormak işe yarıyor" bilgisi, ölçülmüş sayılarla birlikte iddialar olarak birikir ve başka bir modelde yeniden test edilebilir. Başkalarının kendi modellerinde ölçtükleri de aynı sayfaya eklenir

## Siz ne yaparsınız

- **Neyin karara bağlanacağını yazın.** Birkaç soruya cevap verin, bir karar dosyası ortaya çıkar (`shikii new`, etkileşimli olarak ya da `--type --question --option --input --pii` ile; biçimin tamamı [spec başvuru belgesinde](../../skills/shikii/references/spec.md))
- **Cevapları kontrol edin.** Sayfa, yapay zekânın emin olamadığı kararları ve kendi başına hareket ettiği her 20 karardan 1'ini gösterir; siz aslında doğru olanın düğmesine basarsınız. 1'den 9'a kadar tuşlar ve Enter (yapay zekânın seçimiyle aynı) hızlı ilerletir. "Cevaplandı" ile önceki bir cevabı düzeltebilirsiniz (en yenisi geçerlidir). "Ayrıntılar", "yapay zekâya tekrar sor"u ekler: aynı metin bugünün soru biçimi ve çizgisiyle yeniden karara bağlanır, önce ve sonra yan yana gösterilir; kayıtlar sona eklenir, hiç silinmez
- **Ara sıra "hâlâ demek istediğim bu mu?" diye onaylayın.** Makine daha iyi puan alan bir soru biçimi bulduğunda, anlamın değişip değişmediğine bakar ve benimse ya da reddet'e basarsınız

Geri kalan her şey makinenin işidir.

## Kurulum

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # Go 1.27 veya daha yenisi gerekir
cd <proje kök dizini> && shikii init
```

`shikii init` tek bir klasörü, `.shikii/`yi kurar (içinde spec'leriniz için `decisions/`, bilgi için `wiki/`, kayıtlar ve düzeltmeleriniz için `records/` ve `labels/`; proje kök dizinine başka bir şey eklenmez) ve aynı anda ajan bağlantılarını hazırlar: Claude Code (`.mcp.json`), Cursor (`.cursor/mcp.json`), OpenCode (`opencode.json`) ve Codex (`~/.codex/config.toml`) için shikii MCP sunucusu, `AGENTS.md` içinde bir bölüm ve `~/.claude/skills/shikii` içinde ayrıntılı kurallar (skill). Kendi girdilerinize hiç dokunulmaz (var olan bir shikii girdisi olduğu gibi bırakılır). Yalnızca skill klasörü her seferinde yenilenir, bu yüzden onu elle düzenlemeyin. `wiki/` ortak bilgiyi alır (her soru türünün nasıl sorulacağı, işe yaramış yazım kalıpları, her modelin alışkanlıkları); soru biçimi iyileştiricisi (`tune --propose`) onu okur ve düzenlediğiniz sayfalar sonraki bir `init` ile asla üzerine yazılmaz. Go yok mu? Aşağıdaki Docker'ı kullanın.

Girdiler yerel bir modele işaret eder (`shikii up`, port 8080). Yalnızca barındırılan model mi? Dört dosyanın her birinden `--local`ı elle kaldırın. Kullanmadığınız araçlar için de yapılandırma dosyaları oluşturulur; silin ya da `.gitignore`a ekleyin (ikinci bir `shikii init` var olana asla dokunmaz). `.shikii/decisions/`, `.shikii/wiki/` ve `.shikii/labels/` klasörlerini commit edin (spec'leriniz, bilgi ve düzeltmeleriniz: model değişikliğinden sağ çıkan varlıklar). `.shikii/records/` (her karar) büyümeye devam eder, bu yüzden commit etmek isteğe bağlıdır (`shikii gc` eskileri atar). Codex'te zaten `mcp_servers` varsa hiçbir şey eklenmez ve yapıştırılacak satırlar yazdırılır. Yalnızca `CLAUDE.md` olan bir proje `AGENTS.md` almaz (OpenCode `CLAUDE.md`yi okumayı bırakırdı).

Cevap sayfasının kendisini (`web/`, Svelte) değiştirmek Node 24 gerektirir (`make web`). shikii'yi kullanmak gerektirmez: derlenmiş sayfa commit edilmiştir ve ikili dosyanın içine gömülüdür.

## En kısa yollar

- **Docker**: Go yok, model yok mu? `docker compose up` yerel bir model (Kev-4B) ve shikii'yi başlatır; cevap sayfası http://127.0.0.1:8095 adresindedir ([ayrıntılar](../../integrations/docker.md))
- **Hazır kararlar**: bu deponun `decisions/` klasöründe altı tane (istek türü, yinelenen hata raporu, aynı başvuru, …). Birini kendi `.shikii/decisions/` klasörünüze kopyalayın ya da `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` ile ondan başlayın ([liste](../../decisions/README.md))
- **Elinizde zaten geçmiş kararların bir tablosu var**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` onu düzeltmelere çevirir, böylece ilk 50'yi elle cevaplamazsınız
- **Kendi programınızdan**: `shikii serve`i başlatın ve `POST /v1/decide` (karar) ile `POST /v1/label` (düzeltme) çağırın. Gövde JSON'dur ve `-H 'Content-Type: application/json'` zorunludur (tüm uç noktalar: [uzun kılavuz](../guide.md#or-run-one-process-for-programs-and-people))
- **Başkasının örneği**: [başvuru e-postalarını yönlendirme](../examples/inquiry-route.md) (Japonca). İlk kez kullanan biri yalnızca README ile tüm turu tamamladı; spec ve 22 girdi dahil
- **Yapay zekânın kendi düşüncesini ölçün** (Claude Code ve Codex): `shikii hook install`, her turdan önce ve sonra shikii'yi çalıştıran iki hook ekler. Önce: bu istek sonunda dosyaları değiştirecek mi ve ajan önce bir yaklaşım üzerinde anlaşmalı mı? Sonra: değişiklikler istediğinizin sınırları içinde miydi ve istediğinizi bitirdi mi? İlk doğru cevap, çalışma ağacının değişip değişmediğinden bedavaya gelir; gerisini bir sayfada siz cevaplarsınız. Bir çizgi oluşana kadar hiçbir şey değişmez. Sonra plan gerektiren bir istek ajana bir not olur; bitmemiş ya da kapsam dışı görünen bir tur ise size tek satırlık bir mesaj olur, çizginin kaç doğru cevaba dayandığı ve insan tarafından kontrol edilmiş kesinlik ile birlikte. Hiçbir hook hiçbir zaman bir aracı durdurmaz ([ayrıntılar](../../integrations/claude-code.md), Japonca)
- **CI içinde**: `uses: hayaenfj/shikii@main` her pull request'te `shikii check` çalıştırır, böylece değişen bir soru biçimi ya da zayıflayan bir çizgi derlemeyi düşürür

## Tam bir tur

```sh
shikii up                                                             # yerel modeli başlat (ilk çalıştırmada indirilir)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (henüz çizgi yok, o yüzden: bir insana sor)
shikii serve --local http://127.0.0.1:8080                                   # spec'ler ve bilgi .shikii/ içinden okunur
#   → http://127.0.0.1:8095 adresinde cevaplayın. Bir karar istediği sayıya ulaşınca (eval.min_n, bunun için 20) çizgi kendiliğinden çizilir
#     Daha az cevapla denemek için spec'teki eval.min_n değerini elinizdekine düşürün (--force ile yazılan bir çizgi check tarafından işaretlenir)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (güvenle yapılabilir, kanıtıyla: çizgi 61 düzeltmeye dayanıyor, 48'i insanlardan. Bu çizginin act tarafına koyduğu insan cevaplarından, 25 tane, %96'sı doğruydu)
```

Yalnızca dört bant vardır. `act` = devam et, `ask` = bir insana danış, `discard` = hiçbir şey yapma, `abstain` = girdi eksik. Hiçbir zaman ham bir olasılık okumazsınız.

## Yeni olan ne

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="shikii mimarisi: insanların yaptığı üç şey, varlıklar (sorular, doğru cevaplar, makbuzlar), makinenin büyüttüğü döngü, doğrulanabilir wiki, kullan-at modeller" width="100%"></p>

- **Bilgi modelde değil, sorularda ve düzeltmelerde birikir.** Bu yüzden model kullan-attır ve değiştirmek insan emeği gerektirmez. Ölçüldü: soru biçimini ve düzeni makine seçip benzer örnekler eklediğinde, sıradan bir temel model doğrulukta ince ayara (LoRA) yetişti ([sayılar](../bench.md))
- **Otomatik kararlar kanıtla gelir.** "%95 eminim" değil, "insanların kontrol ettiği 48 cevapta kesinlik 0,96"
- **Wiki okunmaz, ölçülür.** "Böyle sormak işe yarıyor" bilgisi sayılarla birlikte iddialar olarak birikir ve yeni modellerde yeniden test edilir. Bir iddianın doğrulandığına yalnızca bir insan karar verir

## Daha fazla okuma

- [Uzun kılavuz](../guide.md): her komut, tam hızlı başlangıç, HTTP API, neleri bağlayabileceğiniz, ölçülen sonuçlar ve SSS · [spec başvuru belgesi](../../skills/shikii/references/spec.md)
- [Tasarım notları](../decision-ops-framework.md): çerçevenin neyi sahiplendiği ve neyi sahiplenmeyi reddettiği, nelerin doğrulandığı · [Mimari](../architecture.md) (Japonca)
- [Hazır kararlar](../../decisions/README.md) · [nasıl sorulur wiki'si](../../wiki/index.md) · [kıyaslama](../bench.md) · [model karşılaştırması](../model-comparison.md) · [işletim günlüğü](../operations.md)
- [Bağlantılar](../../integrations/mcp.md): MCP, sıradan bir sohbet modelini karar modeline çeviren uyarlayıcı, Docker, Claude Code
- [Japonca kılavuz](../guide.ja.md), gerçek hayattan tam bir turla

## Katkıda bulunma

Issue'lar ve pull request'ler memnuniyetle karşılanır. Bir davranışı koruyan bir test önce başarısız olurken gösterilir: davranışı bilerek bozun, testi çalıştırın, nasıl düştüğünü kaydedin, sonra düzeltin ve geçtiğini kaydedin. Belgelerdeki her sayı bir betikten gelir. Bir wiki iddiasının durumunu (`confirmed` / `tentative` / `rejected`) yalnızca insanlar değiştirir, kanıt olan çalıştırmayla birlikte. Yeni seçenek yok: yerleşim (proje kök dizininde `decisions/` `wiki/` `.shikii/`) ve sabit kurallar işi görür.

## Lisans

[Apache 2.0](../../LICENSE). Model lisansları ayrıdır: Kev-4B ve Bonsai ailesi Apache 2.0, OpenJev CC BY-NC 4.0, barındırılan Jev TypeSafe'in hizmetidir. shikii olasılıklarınızı ve etiketlerinizi saklar, modellerden hiçbir şey saklamaz.
