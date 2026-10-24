[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · **Türkçe** · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: yapay zekânın "muhtemelen" demesini, üzerine iş kurabileceğiniz bir "güvenle kullanılabilir"e çevirin

### Bir yapay zekâya "evet mi hayır mı", "hangisi" ve "ne kadar" sorularını karar verdiren ve onu sizin düzeltmelerinizle büyüten bir araç. Sizin tek yaptığınız, bir sayfada aslında neyin doğru olduğunu cevaplamaktır.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-381%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../decision-ops-framework.md)

[Sorun](#sorun) · [Kullanım 1: yapay zekânın kendi düşüncesini ölçün](#kullanım-1-yapay-zekânın-kendi-düşüncesini-ölçün) · [Kullanım 2: bir iş kararını ona bırakın](#kullanım-2-bir-iş-kararını-ona-bırakın) · [Sayfada ne yaparsınız](#sayfada-ne-yaparsınız) · [Kurulum](#kurulum) · [Yapay zekâyı neden değiştirebilirsiniz](#yapay-zekâyı-neden-değiştirebilirsiniz) · [Daha fazla okuma](#daha-fazla-okuma)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## Sorun

Bir yapay zekâya "bu başvuru öncekiyle aynı konu mu?" diye sorun, "muhtemelen, 0,9" der. İş için bu yeterli değildir. Bu 0,9 gerçekten 10 seferin 9'unda doğru mu? Daha yeni bir yapay zekâya geçtiğiniz gün sessizce yalana dönüştü mü? Kimse bakmazken verdiği kararlar hâlâ doğru mu? Her seferinde bir insan kontrol edecekse, yapay zekâ kullanmanın pek anlamı kalmamıştır.

shikii, düzeltmelerinizden bir **çizgi** çizer: bunun üstünde yapay zekâ kendi başına hareket edebilir. Bir çizgi oluşana kadar hiçbir şeye güvenilmez. Oluştuktan sonra her otomatik karar kendi dayanağını taşır: çizginin kaç düzeltmeye dayandığı ve ne sıklıkla doğru çıktığı. Çizgi yapay zekânın içinde değil, sorunun ve düzeltmelerin yanında yaşar; bu yüzden yapay zekâyı değiştirmek insan emeği gerektirmez.

İki kullanım yolu var: kodunuzu yazan yapay zekânın düşüncesini ölçmek (en hafifi) ve ona işinizden bir kararı bırakmak.

## Kullanım 1: yapay zekânın kendi düşüncesini ölçün

Claude Code ya da Codex kullananlar için. Kod yazan ajan, bir istek aldığında ve işi bitirdiğinde kararlar verir; shikii bunları sessizce ölçer.

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

Hepsi bu. İkili dosyayı kurar, Claude Code ve Codex'e iki hook ekler, ajanın kurallarını (bir skill) yerine koyar ve arka planda küçük bir yerel yapay zekâ (Kev-4B) başlatır (llama.cpp yoksa, onu kurmak için gereken tek satırı yazdırır). Yeni bir terminal açın ve ajanla her zamanki gibi çalışmaya devam edin (Codex'te shikii'nin hook'larına güvenmek için bir kez `/hooks` çalıştırın). Dört şey ölçülür.

| Ne zaman | Soru | Doğru cevap nereden gelir |
|---|---|---|
| Bir istek geldiğinde | Bu istek sonunda dosyaları değiştirecek mi? | Otomatik: bir sonraki isteğe kadar çalışma ağacı değişti mi |
| Bir istek geldiğinde | Ajan bir şeye dokunmadan önce sizinle bir yaklaşım üzerinde anlaşmalı mı? | Siz, sayfada |
| Tur bittiğinde | Değişiklikler istediğinizin sınırları içinde miydi? | Siz, sayfada |
| Tur bittiğinde | İstediğinizi bitirdi mi? | Siz, sayfada |

Bir turun sonunda tek bir satır belirir, `shikii: bu oturumun 3 kararı cevabınızı bekliyor: http://127.0.0.1:8096/`, ve bir dakikanız olduğunda açarsınız (hook sayfayı da arka planda başlatır); her cevap saniyeler alır. 50 cevaptan sonra bir çizgi çizilir ve ajanın davranışı değişir: plan gerektiren bir istek ajana bir not olur; bitmemiş ya da kapsam dışı görünen bir tur ise size tek bir satır olur. Her biri "61 doğru cevaba dayanan çizgi, insan tarafından kontrol edilmiş kesinlik 0,96" gibi bir dayanakla gelir. O zamana kadar hook'lar yalnızca kayıt tutar. Hiçbir hook hiçbir zaman bir aracı durdurmaz.

Her şey tek bir yerde, `~/.shikii/agent` içinde yaşar; tüm projeleriniz boyunca her soru için tek bir çizgi vardır (sorular ajanın düşüncesi hakkındadır, bu yüzden doğru cevap her depoda aynıdır). Her kayıt, geldiği depoyla işaretlenir ve sayfa buna göre filtreleyebilir. Ayrıntılar: [hook](../../integrations/claude-code.md) (Japonca).

## Kullanım 2: bir iş kararını ona bırakın

"Bu başvuru aynı konu mu?" ya da "bu mesaj bir değişiklik isteği mi?" gibi işte tekrarladığınız kararlar için: yapay zekâ karar versin ve onu düzeltmelerinizle büyütün. Burada da yazılacak bir komut yoktur. Ajana söylersiniz.

> Bir başvurunun öncekiyle aynı konu olup olmadığına shikii'nin karar vermesini ve bunu düzeltmelerimle büyütmesini istiyorum

Ajan kurallarını (skill'i) izler: projede `.shikii/` oluşturur (`shikii init`), soruyu, seçenekleri ve girdileri belirleyip karar dosyasını yazar (`shikii new`; soruyu ve seçenekleri size bir kez gösterir, çünkü bu sizin niyetinizdir), kararı çağırır (`shikii decide`), cevap sayfasını başlatır (http://127.0.0.1:8095) ve size haber verir. Bir çizgi oluşana kadar hiçbir karar `act` (devam et) olmaz. Sayfada cevap verdikçe, kararın kendi sayısına ulaşıldığında (bu örnekte 20) çizgi otomatik olarak çizilir ve o andan itibaren:

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (güvenle yapılabilir. Çizgi 61 doğru cevaba dayanıyor, 48'i insanlar tarafından verilmiş; insanların cevapladıklarından bu çizginin act tarafına koyduğu 25 tanesinin %96'sı doğruydu)
```

Yalnızca dört `band` değeri vardır: `act` = devam et, `ask` = bir insana danış, `discard` = hareket etme, `abstain` = girdi yetersiz. Hiçbir zaman ham olasılıklara göre hareket etmezsiniz.

- **Elle**: `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`. Hepsi [kılavuzda](../guide.md)
- **Bir ajandan**: `shikii init` MCP ayarını yazar, böylece Claude Code, Cursor, OpenCode ve Codex `shikii_decide` ve `shikii_label` araçlarını doğrudan çağırabilir ([MCP](../../integrations/mcp.md))
- **Kendi programınızdan**: `shikii serve`i başlatın ve `POST /v1/decide` (karar) ile `POST /v1/label` (düzeltme) çağırın. Gövde JSON'dur ve `-H 'Content-Type: application/json'` gerekir ([HTTP API](../guide.md#or-run-one-process-for-programs-and-people))
- **Hazır kararlar**: bu deponun `decisions/` klasöründe altı tane (istek türü, yinelenen hata raporu, aynı başvuru, …). Birini `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` ile kopyalayın ve kendi sözcüklerinizle yeniden yazın ([liste](../../decisions/README.md))
- **Elinizde zaten geçmiş kararların bir tablosu var**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` onu düzeltmelere çevirir, böylece ilk 50'yi elle cevaplamanız gerekmez
- **Docker**: henüz Go da model de yok mu? `docker compose up` yerel bir yapay zekâ ve shikii'yi başlatır; sayfa http://127.0.0.1:8095 adresindedir ([ayrıntılar](../../integrations/docker.md))
- **Başkasının örneği**: [başvuru e-postalarını yönlendirme](../examples/inquiry-route.md) (Japonca). İlk kez kullanan biri yalnızca README ile tüm turu tamamladı; karar dosyası ve 22 girdi dahil

## Sayfada ne yaparsınız

Cevap sayfasında (`shikii serve`) bir insan üç şey yapar. Geri kalan her şey makinenin işidir.

1. **Cevaplayın.** Sayfa, yapay zekânın emin olamadığı kararları ve kendi başına hareket ettiği her 20 karardan 1'ini gösterir; siz aslında doğru olanın düğmesine basarsınız. 1'den 9'a kadar tuşlar ve Enter (yapay zekânın seçimiyle aynı) hızlı ilerletir. "Cevaplandı" ile önceki bir cevabı düzeltebilirsiniz (en yenisi geçerlidir)
2. **Yeni bir soru biçimi yazın.** "Böyle sorsak daha iyi olur" diye düşünüyorsanız, "Soru biçimi" panelinde kendi sözcüklerinizle yazın. Makine onu aynı düzeltmeler üzerinde mevcut soru biçimiyle karşılaştırıp puanlar (ajanın kendi kararları için her 50 yeni cevapta otomatik olarak) ve yalnızca gerçekten daha iyiyse "benimsenebilir" der. Soru biçimi önermeyi bir yapay zekâya da bırakabilirsiniz. Hiçbir zaman bir dosya açmazsınız
3. **Benimse ya da reddet'e basın.** Anlamın değişip değişmediğine bakar ve basarsınız. Sayıları makine kontrol eder; anlamı siz kontrol edersiniz

"Ayrıntılar", model başına çizginin durumunu, "yapay zekâya tekrar sor"u (aynı metni bugünün soru biçimi ve çizgisiyle yeniden karara bağlayıp öncesini ve sonrasını görmek) ve bilgi panelini ("böyle sormak işe yarıyor" biçimindeki iddialar, kanıtlarıyla) ekler. Kayıtlar sona eklenir, hiç silinmez; yeni bir karar eskisinin yerini alır.

## Kurulum

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 veya daha yenisi; yoksa Docker (yukarıda)
```

install.sh'ın yaptığı: ikili dosyaları (`shikii` ve `shikii-readout`) kurar, Claude Code ve Codex'e iki hook ekler, ajanın kurallarını (skill'i) `~/.claude/skills/shikii` içine koyar ve llama.cpp varsa yerel yapay zekâyı (Kev-4B, :8080 üzerinde) arka planda başlatır. Bunun yerine barındırılan Jev ya da OpenRouter kullanmak için anahtarı bir ortam değişkenine koyun ([bağlantılar](../guide.md#what-you-can-connect)). Neyin çalıştığını ve neyin eksik olduğunu `shikii doctor` söyler.

Proje başına kurulum (`shikii init`) ajanın işidir (kendiniz de çalıştırabilirsiniz). Tek bir klasör oluşturur, `.shikii/` (içinde: `decisions/` = karar dosyaları, `wiki/` = bilgi, `records/` ve `labels/` = kayıtlar ve düzeltmeler). Proje kök dizininde başka hiçbir şey belirmez. `decisions/`, `wiki/` ve `labels/` yapay zekâ değişikliğinden sağ çıkan varlıklardır, bu yüzden onları commit edin; `records/` büyümeye devam eder, bu yüzden commit etmek isteğe bağlıdır (`shikii gc` eskileri atar). Ayrıca ajanlarınızın ayarlarına (Claude Code'un `.mcp.json`u, Cursor'ın `.cursor/mcp.json`u, OpenCode'un `opencode.json`u, Codex'in `~/.codex/config.toml`u) sizin yazdığınız girdilere dokunmadan bir shikii girdisi ekler. Tekrar çalıştırmak var olan hiçbir şeyi değiştirmez.

Komut çıktısı ve hook mesajları İngilizcedir (Claude Code'un dil ayarı Japonca ise hook mesajları İngilizcenin yanında Japonca da gösterilir). Sayfada Japonca vardır. Yalnızca barındırılan bir yapay zekâ kullanmak, `AGENTS.md` ile `CLAUDE.md` farkı ve sayfanın kendisini değiştirmek için Node gibi ince ayrıntılar [kılavuzdadır](../guide.md).

## Yapay zekâyı neden değiştirebilirsiniz

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="shikii mimarisi: insanların yaptığı üç şey, varlıklar (sorular, doğru cevaplar, makbuzlar), makinenin büyüttüğü döngü, doğrulanabilir wiki, kullan-at model" width="100%"></p>

- **Bilgi yapay zekânın içinde değil, soruyla ve düzeltmelerle birlikte birikir.** Düzeltmeler yapay zekâdan sağ çıkar, bu yüzden yeni bir yapay zekâ onları olduğu gibi kullanabilir ve çizgi yeniden çizilir. Ölçümlerimizde, soru biçimini ve düzeni makinenin seçmesi ve benzer örnekler eklemesi, modelin ağırlıklarına ince ayar yapmakla (LoRA) aynı doğruluğa ulaştı ([sayılar](../bench.md))
- **Otomatik kararlar bir makbuz taşır.** "%95 eminim" değil, "insanların kontrol ettiği 48 durumda kesinlik 0,96"
- **Wiki okunmaz, ölçülür.** "Böyle sormak işe yarıyor" bilgisi sayılarla birlikte iddialar olarak birikir ve yeni bir yapay zekâda yeniden test edilebilir. Bir iddianın doğrulandığına yalnızca bir insan karar verir

## Daha fazla okuma

- [Kılavuz](../guide.md): her komut, HTTP API, SSS · [spec başvuru belgesi](../../skills/shikii/references/spec.md)
- [Hook](../../integrations/claude-code.md) (Japonca): ajanın kendi düşüncesinin nasıl ölçüldüğü, bant → davranış tablosu, iç yapı
- [Mimari](../architecture.md) (Japonca): yukarıdaki şemanın arkasındaki bütün resim
- [Hazır kararlar](../../decisions/README.md) · [nasıl sorulur wiki'si](../../wiki/index.md) · [ölçülen sayılar](../bench.md)
- [Bağlantılar](../../integrations/mcp.md): MCP, sıradan sohbet modelleri için uyarlayıcı, Docker

**Katkıda bulunanlar için**: bir testi bilerek bozun ve düzeltmeden önce düştüğünü görün. Her sayı bir betikten gelir. Bir wiki iddiasını yalnızca bir insan doğrular. Ayar eklemeyin; bunun yerine sabit yerler ve sabit kurallar.

## Lisans

[Apache-2.0 OR MIT](../../LICENSE). Model lisansları ayrıdır (Kev-4B ve Bonsai ailesi Apache 2.0, OpenJev CC BY-NC 4.0, barındırılan Jev TypeSafe'in hizmetidir).
