# Argos çeviri modelleri ve doğrulama kaynağı

Doğrulama tarihi: **2026-11-04**. Modeller temel uygulama paketinde bulunmaz; her yön kullanıcı isteğiyle indirilir. Çeviri sırasında internet kullanılmaz.

| Yön | Paket sürümü | Tam dosya boyutu | SHA-256 |
| --- | --- | ---: | --- |
| Türkçe → İngilizce | 1.5 | 120561223 bayt | `15217905586561fac843efb5730aabff9a1534d5a9aa3f6ebcc0f9641394ac2d` |
| İngilizce → Türkçe | 1.5 | 124742526 bayt | `2d553a00880a0c21dea5b1c375535659e6bf18c9bf2ce9847c2a2fd2ff50316a` |

## Kaynaklar

- [Türkçe → İngilizce paketi](https://data.argosopentech.com/argospm/v1/translate-tr_en-1_5.argosmodel)
- [İngilizce → Türkçe paketi](https://data.argosopentech.com/argospm/v1/translate-en_tr-1_5.argosmodel)
- [Resmî paket indeksi](https://raw.githubusercontent.com/argosopentech/argospm-index/main/index.json)
- [Argos Translate kaynak kodu](https://github.com/argosopentech/argos-translate)

İki URL gerçek tam GET ile indirilip hash'lendi; karşılık gelen `https://argos-net.com/v1/` kopyalarının boyut ve hash değerleri de eşleşti. Parent doğrulaması dosyaları yeniden hash'leyip ZIP içindeki metadata'yı okudu. Her pakette 12 ZIP girdisi bulunur; açılmış toplam boyutlar sırasıyla 134945874 ve 135000437 bayttır. Her iki `metadata.json` dosyası `package_version: 1.5`, `argos_version: 1.5` ve doğru `from_code`/`to_code` çiftini içerir.

## Güven sınırı

Upstream bu dosyalar için yayımlanmış veya imzalı checksum sağlamıyor. Yukarıdaki değerler upstream tarafından imzalanmış değerler değil, HTTPS üzerinden alınan gerçek dosyaların projede sabitlenen SHA-256 değerleridir. İki sunucuda aynı dosya olması bağımsız kriptografik kaynak doğrulaması değildir. Sabit boyut/hash sonraki bozulma veya dosya değişimini reddeder; ilk kaynak ediniminin güven kökü HTTPS ve incelenen sabitlemedir.

## Lisans

Argos yazılımının lisansı ile model ağırlıklarının lisansı yalnızca kod lisansından çıkarılmadı. Bu iki model paketinde lisans dosyası veya metadata lisans alanı bulunmadı. Ancak proje geliştiricisi **PJ-Finlay**, [2 Ağustos 2026 tarihli açık beyanında](https://github.com/argosopentech/argos-translate/issues/533#issuecomment-5160080718) “Yes the .argosmodel model binaries are released under the same MIT/CC0 license.” diyor. Bu, model ikilileri için upstream'in MIT/CC0 beyanıdır; paket içinde bir lisans dosyası veya paket özelinde imzalı bir izin değildir.

[2021 tarihli önceki açıklama](https://github.com/argosopentech/argos-translate/issues/76#issuecomment-815704991) model eğitimi çalışmasının aynı lisans altında sayıldığını belirtirken bazı eğitim verilerinin açık lisans belirsizliğini de kabul eder. Eğitim veri kümelerinin hakları ve türetilmiş model yükümlülükleri için paket özelinde kapsamlı bir doğrulama bulunamadı; bu kalan risk teslim raporunda korunur. Paket README'leri ve veri kümesi atıfları kurulum sırasında korunur. Modeller temel uygulama dağıtımına gömülmez.

Kullanılan yayımlanmış Argos Translate sürümü **1.11.0**'dır. Kaynak `LICENSE` dosyası MIT'dir; README ayrıca MIT/CC0 çift lisans beyan eder. Dağıtımın yazılım lisansı/bağımlılık envanteri motor paketinin notice dosyalarında ayrıca yer alır.

`data.argosopentech.com/argospm/v1/index.json` 404 döndürdüğünden çalışma zamanı bu adrese veya güncel indekse dayanmaz. Yalnızca incelenmiş sabit paketler indirilir; katalog/model güncellemesi kendiliğinden yapılmaz.

## Çeviri kalitesi sınırı

Bu 1.5 modelleri makine çevirisidir; doğru sonuç garantisi vermez. Gerçek testte `Türkçe öğreniyorum.` cümlesi tr→en yönünde yanlış olarak `I learn English.` üretmiştir. Bu motor/bağlantı arızası değil, model çıktısındaki anlam hatasıdır. Tıbbi, hukuki veya güvenlik açısından kritik metinler bağımsız olarak kontrol edilmelidir. Daha yeni bir model otomatik olarak indirilmez; model değişikliği yeni kaynak/hash/uyumluluk doğrulaması gerektirir.
