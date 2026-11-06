# Engelsiz’in vizyonu

> Bilginin erişim engeli olmaz.

Bu belge projenin uzun vadeli amacını anlatır. Bugün kullanılabilen özellikler ve indirme bağlantıları için [README](../README.md) dosyasına bakın.

Engelsiz; internet bağlantısının olmadığı, kesildiği veya bilgiye erişimin teknik ya da idari nedenlerle kısıtlandığı durumlarda bilgiye erişimi sürdürebilmek için geliştirilen açık kaynaklı, offline-first bir projedir.

Amaç; Wikipedia, eğitim içerikleri, haritalar, acil durum kaynakları ve diğer açık bilgi arşivlerini yerel olarak saklayabilen, bunlar üzerinde arama yapabilen ve yerel çalışan yapay zekâ modelleriyle etkileşime geçebilen bağımsız bir sistem oluşturmaktır.

**İnternet kesilebilir.**

**Platformlar kapanabilir.**

**Kaynaklara erişim engellenebilir.**

**Bilgi kalır.**

---

## Neden Engelsiz?

İnternet bize tarihte hiç olmadığı kadar büyük bir bilgiye erişim imkânı sağlıyor.

Fakat bu erişim sandığımız kadar kalıcı değil.

Bir internet bağlantısı kesilebilir. Bir servis kullanılamaz hale gelebilir. Bir web sitesi kapanabilir. Bir alan adına veya platforma erişim kısıtlanabilir. Bir doğal afet iletişim altyapısını devre dışı bırakabilir.

Böyle durumlarda bilgi hâlâ var olsa bile **ona ulaşma imkânımız olmayabilir.**

Engelsiz'in çıkış noktası tam olarak burada:

> **Bilgiye erişim, sürekli bir internet bağlantısının veya tek bir merkezi platformun varlığına bağlı olmamalı.**

İhtiyaç duyduğumuz temel bilgi kaynaklarını kendi cihazlarımızda saklayabilmeli ve dışarıdaki bağlantının durumundan bağımsız olarak kullanabilmeliyiz.

Engelsiz herhangi bir erişim engelini aşmaya yönelik bir proxy veya VPN değildir.

Bunun yerine farklı bir yaklaşım benimser:

**Bilgiyi önceden yerelleştirir.**

Kaynak cihazınızdaysa ona ulaşmak için uzaktaki bir sunucunun, DNS servisinin veya sürekli çalışan bir internet bağlantısının varlığına ihtiyaç duymazsınız.

---


## Bugünkü kapsam ve gelecek

Engelsiz 1.0.0 Wikipedia, harita, çeviri, eğitim ve isteğe bağlı yerel AI asistanını bir araya getirir. Aşağıdaki başlıklar teslim edilen kapsamı ve genişletilmesi planlanan alanları ayırır.

### 🗺️ Haritalar

Türkiye için offline harita ve OpenStreetMap verileri. **v0.1.0-alpha.3 ile teslim edildi:** 81 il, ayrı indirilebilir Z0–Z15 paketleri, çevrimdışı Türkçe etiketli görüntüleme.

### 🚑 Acil Durum

Afet hazırlığı, ilk yardım ve güvenilir acil durum kaynakları. Eğitim Merkezi'nde başlangıç başvuruları vardır; Türkçe resmî ilk yardım içeriğinin çevrimdışı kapsamı, güncellik ve kullanım izni doğrulaması nedeniyle henüz tamamlanmamıştır.

### 🎓 Eğitim

**Uygulandı:** Offline Eğitim Merkezi; dört kategori, kullanıcı seçimiyle indirilen PDF/ZIM/HTML5 kaynakları, Türkçe Vikikitap ve üç Türkçe PhET simülasyonu. Kaynak/lisans bilgileri, güvenli kurulum, kaldırma ve gerçek ağsız yeniden başlatma testi [modül raporunda](offline-education-center.md) kayıtlıdır. Daha geniş Türkçe içerik ve Kolibri, doğrulanmış entegrasyon olmadan varmış gibi sunulmaz.

### 📚 Bilgi Arşivleri

Wikipedia dışında farklı açık bilgi kaynakları.

### 🤖 Yerel Yapay Zekâ

**1.0.0 ile uygulandı:** isteğe bağlı Qwen3 Lite/Balanced/Quality GGUF modelleri, cihazda llama.cpp inference, kurulu ZIM/HTML kaynaklarında retrieval, gerçek kaynak bağlantıları, Türkçe streaming yanıt ve durdurma. Bulut inference ve agent otomasyonu yoktur. [Kurulum, mimari ve sınırlar](offline-ai.md).

### 📦 Bilgi Paketleri

Kullanıcıların ihtiyaçlarına göre farklı içerik paketleri indirebilmesi.

Örneğin:

```text
core
wikipedia-tr
maps-tr
emergency
education
```

Böylece herkes devasa bir arşivi indirmek zorunda kalmadan ihtiyaç duyduğu bilgileri cihazında saklayabilir.

---

## Temel İlkeler

### Offline First

Temel özellikler internet bağlantısı olmadan çalışabilmelidir.

İnternet Engelsiz için bir gereksinim değil, yalnızca bilgi paketlerini güncellemenin yollarından biridir.

### Local First

Bilgi ve yapay zekâ mümkün olduğunca kullanıcının kendi cihazında çalışmalıdır.

### Açık Kaynak

Engelsiz'in nasıl çalıştığı incelenebilmeli, geliştirilebilmeli ve topluluk tarafından sürdürülebilmelidir.

### Açık Bilgi

Mümkün olduğunca açık lisanslı ve yeniden dağıtılabilir bilgi kaynakları kullanılmalıdır.

### Kaynak Gösterme

Yapay zekâ bir bilgi verdiğinde mümkün olduğunca bu bilginin hangi yerel kaynaktan geldiği görülebilmelidir.

### Merkeziyetsizlik

Temel bilgiye ulaşmak için tek bir şirkete, servise veya uzak sunucuya bağımlı olunmaması hedeflenmektedir.

### Basit Kurulum

Engelsiz'i çalıştırmak için ileri düzey sistem yönetimi bilgisine ihtiyaç duyulmaması hedeflenmektedir.

---

## Kullanım Senaryoları

Engelsiz özellikle şu durumlar düşünülerek geliştirilmektedir:

* İnternet bağlantısının olmadığı bölgeler
* Uzun süreli internet veya altyapı kesintileri
* Doğal afetler
* Belirli web sitelerine veya bilgi kaynaklarına erişimin kısıtlandığı dönemler
* İnternet erişiminin geçici olarak sınırlandırıldığı durumlar
* Çevrimiçi servislerin kullanılamaz hale gelmesi
* Kırsal bölgeler
* Seyahat ve saha çalışmaları
* İnternetten bağımsız kişisel bilgi arşivleri
* Raspberry Pi veya mini PC tabanlı offline bilgi sunucuları

---

## Engelsiz Ne Değildir?

Engelsiz;

* VPN değildir.
* Proxy değildir.
* Anonimlik aracı değildir.
* İnternet erişim engellerini teknik olarak aşmayı amaçlayan bir araç değildir.

Engelsiz'in yaklaşımı daha basittir:

> **İhtiyaç duyabileceğin bilgiyi, ihtiyaç duymadan önce yanında tut.**

---

## Katkıda bulunmak

[Katkı rehberi](../CONTRIBUTING.md) üzerinden başlayabilirsiniz.
