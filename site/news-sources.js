/**
 * Global live news / weather / public-access catalog for livenews.html.
 * Mirrors + expands news_wall.go MajorNewsSources / ExtendedNewsSources.
 * kind: news | weather | public | earthcam
 * Streams may geo-restrict or go offline; wall keeps posters + refresh.
 */
(function (global) {
  'use strict';

  function S(id, label, url, region, kind, hue, tags) {
    return {
      id: id,
      label: label,
      url: url,
      region: region,
      kind: kind || 'news',
      hue: hue == null ? 200 : hue,
      tags: tags || [],
    };
  }

  const MAJOR_NEWS = [
    // ── United States · news ──
    S('abc', 'ABC News', 'https://www.youtube.com/@ABCNews/live', 'us', 'news', 0, ['us', 'breaking']),
    S('nbc', 'NBC News', 'https://www.youtube.com/@NBCNews/live', 'us', 'news', 35, ['us', 'breaking']),
    S('cbs', 'CBS News', 'https://www.youtube.com/@CBSNews/live', 'us', 'news', 210, ['us']),
    S('cnn', 'CNN', 'https://www.youtube.com/@CNN/live', 'us', 'news', 0, ['us', 'breaking']),
    S('fox', 'Fox News', 'https://www.youtube.com/@FoxNews/live', 'us', 'news', 20, ['us', 'politics']),
    S('msnbc', 'MSNBC', 'https://www.youtube.com/@MSNBC/live', 'us', 'news', 220, ['us', 'politics']),
    S('bbg', 'Bloomberg', 'https://www.youtube.com/@BloombergTelevision/live', 'us', 'news', 45, ['markets', 'business']),
    S('cnbc', 'CNBC', 'https://www.youtube.com/@CNBC/live', 'us', 'news', 200, ['markets', 'business']),
    S('foxbiz', 'Fox Business', 'https://www.youtube.com/@FoxBusiness/live', 'us', 'news', 30, ['markets']),
    S('cspan', 'C-SPAN', 'https://www.youtube.com/@cspan/live', 'us', 'public', 260, ['politics', 'public']),
    S('cspan2', 'C-SPAN 2', 'https://www.youtube.com/@CSPAN/live', 'us', 'public', 265, ['politics', 'public']),
    S('pbs', 'PBS NewsHour', 'https://www.youtube.com/@PBSNewsHour/live', 'us', 'news', 170, ['us', 'public']),
    S('npr', 'NPR', 'https://www.youtube.com/@npr/live', 'us', 'news', 160, ['us', 'public']),
    S('ap', 'Associated Press', 'https://www.youtube.com/@AP/live', 'us', 'news', 10, ['wire', 'breaking']),
    S('usa', 'USA TODAY', 'https://www.youtube.com/@USATODAY/live', 'us', 'news', 15, ['us']),
    S('nyt', 'New York Times', 'https://www.youtube.com/@nytimes/live', 'us', 'news', 0, ['us']),
    S('wapo', 'Washington Post', 'https://www.youtube.com/@WashingtonPost/live', 'us', 'news', 220, ['us', 'politics']),
    S('politico', 'POLITICO', 'https://www.youtube.com/@politico/live', 'us', 'news', 250, ['politics']),
    S('thehill', 'The Hill', 'https://www.youtube.com/@thehill/live', 'us', 'news', 240, ['politics']),
    S('newsmax', 'Newsmax', 'https://www.youtube.com/@NewsmaxTV/live', 'us', 'news', 25, ['us']),
    S('oan', 'One America News', 'https://www.youtube.com/@OANN/live', 'us', 'news', 5, ['us']),

    // ── Canada ──
    S('cbc', 'CBC News', 'https://www.youtube.com/@CBCNews/live', 'ca', 'news', 200, ['canada']),
    S('ctv', 'CTV News', 'https://www.youtube.com/@CTVNews/live', 'ca', 'news', 210, ['canada']),
    S('globalca', 'Global News', 'https://www.youtube.com/@GlobalNews/live', 'ca', 'news', 195, ['canada']),
    S('cpac', 'CPAC', 'https://www.youtube.com/@CPAC/live', 'ca', 'public', 270, ['canada', 'public', 'politics']),

    // ── Latin America ──
    S('televisa', 'N+ Foro TV', 'https://www.youtube.com/@NMas/live', 'latam', 'news', 140, ['mexico']),
    S('milenio', 'Milenio', 'https://www.youtube.com/@MILENIO/live', 'latam', 'news', 150, ['mexico']),
    S('globo', 'g1 / Globo', 'https://www.youtube.com/@g1/live', 'latam', 'news', 120, ['brazil']),
    S('record', 'Record News', 'https://www.youtube.com/@RecordNews/live', 'latam', 'news', 130, ['brazil']),
    S('tnar', 'TN Argentina', 'https://www.youtube.com/@todonoticias/live', 'latam', 'news', 100, ['argentina']),
    S('c5n', 'C5N', 'https://www.youtube.com/@c5n/live', 'latam', 'news', 110, ['argentina']),
    S('nacion', 'La Nación+', 'https://www.youtube.com/@LANACION/live', 'latam', 'news', 105, ['argentina']),
    S('chv', 'Chilevisión', 'https://www.youtube.com/@chilevision/live', 'latam', 'news', 180, ['chile']),
    S('mega', 'Meganoticias', 'https://www.youtube.com/@meganoticiascl/live', 'latam', 'news', 175, ['chile']),
    S('caracol', 'Noticias Caracol', 'https://www.youtube.com/@NoticiasCaracol/live', 'latam', 'news', 40, ['colombia']),
    S('rcn', 'Noticias RCN', 'https://www.youtube.com/@NoticiasRCN/live', 'latam', 'news', 45, ['colombia']),

    // ── United Kingdom / Ireland ──
    S('bbc', 'BBC News', 'https://www.youtube.com/@BBCNews/live', 'uk', 'news', 200, ['uk', 'breaking']),
    S('sky', 'Sky News', 'https://www.youtube.com/@SkyNews/live', 'uk', 'news', 195, ['uk', 'breaking']),
    S('itv', 'ITV News', 'https://www.youtube.com/@itvnews/live', 'uk', 'news', 190, ['uk']),
    S('gbnews', 'GB News', 'https://www.youtube.com/@GBNewsOnline/live', 'uk', 'news', 25, ['uk']),
    S('rte', 'RTÉ News', 'https://www.youtube.com/@rte/live', 'uk', 'news', 145, ['ireland']),
    S('parl', 'UK Parliament', 'https://www.youtube.com/@UKParliament/live', 'uk', 'public', 255, ['uk', 'public', 'politics']),

    // ── Europe ──
    S('eur', 'Euronews', 'https://www.youtube.com/@euronews/live', 'eu', 'news', 185, ['europe']),
    S('f24', 'France 24 EN', 'https://www.youtube.com/@France24_en/live', 'eu', 'news', 210, ['france', 'europe']),
    S('f24fr', 'France 24 FR', 'https://www.youtube.com/@FRANCE24/live', 'eu', 'news', 215, ['france']),
    S('dw', 'DW News', 'https://www.youtube.com/@dwnews/live', 'eu', 'news', 220, ['germany', 'europe']),
    S('dwde', 'DW Deutsch', 'https://www.youtube.com/@deutschewelle/live', 'eu', 'news', 225, ['germany']),
    S('arte', 'ARTE', 'https://www.youtube.com/@ARTEde/live', 'eu', 'news', 280, ['europe', 'culture']),
    S('tagesschau', 'tagesschau', 'https://www.youtube.com/@tagesschau/live', 'eu', 'news', 230, ['germany']),
    S('rtl', 'RTL aktuell', 'https://www.youtube.com/@RTLde/live', 'eu', 'news', 25, ['germany']),
    S('rai', 'Rai News 24', 'https://www.youtube.com/@Rainews24/live', 'eu', 'news', 350, ['italy']),
    S('skyit', 'Sky TG24', 'https://www.youtube.com/@SkyTG24/live', 'eu', 'news', 200, ['italy']),
    S('rtve', 'RTVE Noticias', 'https://www.youtube.com/@rtve/live', 'eu', 'news', 10, ['spain']),
    S('antena3', 'Antena 3 Noticias', 'https://www.youtube.com/@antena3noticias/live', 'eu', 'news', 15, ['spain']),
    S('nos', 'NOS', 'https://www.youtube.com/@NOS/live', 'eu', 'news', 200, ['netherlands']),
    S('vrt', 'VRT NWS', 'https://www.youtube.com/@vrtnws/live', 'eu', 'news', 190, ['belgium']),
    S('svt', 'SVT Nyheter', 'https://www.youtube.com/@svt/live', 'eu', 'news', 210, ['sweden']),
    S('nrk', 'NRK', 'https://www.youtube.com/@nrk/live', 'eu', 'news', 205, ['norway']),
    S('yle', 'Yle', 'https://www.youtube.com/@yle/live', 'eu', 'news', 195, ['finland']),
    S('dr', 'DR Nyheder', 'https://www.youtube.com/@drdk/live', 'eu', 'news', 180, ['denmark']),
    S('tvp', 'TVP Info', 'https://www.youtube.com/@tvp/live', 'eu', 'news', 200, ['poland']),
    S('polsat', 'Polsat News', 'https://www.youtube.com/@PolsatNewsPL/live', 'eu', 'news', 30, ['poland']),
    S('ct24', 'ČT24', 'https://www.youtube.com/@CT24zive/live', 'eu', 'news', 220, ['czechia']),
    S('rtlhu', 'RTL Híradó', 'https://www.youtube.com/@rtlklub/live', 'eu', 'news', 40, ['hungary']),
    S('rtp', 'RTP Notícias', 'https://www.youtube.com/@rtppt/live', 'eu', 'news', 350, ['portugal']),
    S('ert', 'ERT News', 'https://www.youtube.com/@ERTsocial/live', 'eu', 'news', 200, ['greece']),
    S('europarl', 'European Parliament', 'https://www.youtube.com/@EuropeanParliament/live', 'eu', 'public', 250, ['europe', 'public', 'politics']),

    // ── Middle East ──
    S('aje', 'Al Jazeera English', 'https://www.youtube.com/@AlJazeeraEnglish/live', 'me', 'news', 200, ['me', 'breaking']),
    S('aja', 'Al Jazeera Arabic', 'https://www.youtube.com/@AljazeeraChannel/live', 'me', 'news', 195, ['me', 'arabic']),
    S('alarabiya', 'Al Arabiya', 'https://www.youtube.com/@AlArabiya/live', 'me', 'news', 30, ['me', 'arabic']),
    S('skyar', 'Sky News Arabia', 'https://www.youtube.com/@skynewsarabia/live', 'me', 'news', 190, ['me']),
    S('trthaber', 'TRT Haber', 'https://www.youtube.com/@trthaber/live', 'me', 'news', 0, ['turkey']),
    S('trtw', 'TRT World', 'https://www.youtube.com/@trtworld/live', 'me', 'news', 10, ['turkey', 'world']),
    S('i24', 'i24NEWS', 'https://www.youtube.com/@i24NEWS_EN/live', 'me', 'news', 220, ['israel']),
    S('kan', 'KAN News', 'https://www.youtube.com/@kannens/live', 'me', 'news', 200, ['israel']),
    S('iranintl', 'Iran International', 'https://www.youtube.com/@IranIntl/live', 'me', 'news', 340, ['iran']),

    // ── Africa ──
    S('africa24', 'Africa 24', 'https://www.youtube.com/@AFRICA24TV/live', 'africa', 'news', 40, ['africa']),
    S('cnbcafrica', 'CNBC Africa', 'https://www.youtube.com/@CNBCAfrica/live', 'africa', 'news', 45, ['africa', 'markets']),
    S('enca', 'eNCA', 'https://www.youtube.com/@eNCA/live', 'africa', 'news', 200, ['southafrica']),
    S('sabc', 'SABC News', 'https://www.youtube.com/@SABCNews/live', 'africa', 'news', 210, ['southafrica', 'public']),
    S('ntvke', 'NTV Kenya', 'https://www.youtube.com/@NTVKenya/live', 'africa', 'news', 120, ['kenya']),
    S('citizen', 'Citizen TV KE', 'https://www.youtube.com/@citizentvkenya/live', 'africa', 'news', 130, ['kenya']),
    S('channels', 'Channels TV', 'https://www.youtube.com/@ChannelsTelevision/live', 'africa', 'news', 15, ['nigeria']),
    S('tvc', 'TVC News NG', 'https://www.youtube.com/@tvcnewsng/live', 'africa', 'news', 25, ['nigeria']),
    S('arise', 'Arise News', 'https://www.youtube.com/@AriseNews/live', 'africa', 'news', 200, ['nigeria', 'world']),

    // ── Asia ──
    S('nhk', 'NHK World Japan', 'https://www.youtube.com/@NHKWORLDJAPAN/live', 'asia', 'news', 330, ['japan', 'world']),
    S('nhkjp', 'NHK News', 'https://www.youtube.com/@nhk/live', 'asia', 'news', 335, ['japan']),
    S('ann', 'ANN News', 'https://www.youtube.com/@ANNnewsCH/live', 'asia', 'news', 340, ['japan']),
    S('tbs', 'TBS NEWS DIG', 'https://www.youtube.com/@TBSNEWS_DIG/live', 'asia', 'news', 200, ['japan']),
    S('yonhap', 'Yonhap News', 'https://www.youtube.com/@YonhapNewsTV/live', 'asia', 'news', 210, ['korea']),
    S('yonsei', 'YTN', 'https://www.youtube.com/@ytnnews24/live', 'asia', 'news', 215, ['korea']),
    S('cna', 'CNA', 'https://www.youtube.com/@channelnewsasia/live', 'asia', 'news', 200, ['singapore', 'asia']),
    S('scmp', 'SCMP', 'https://www.youtube.com/@SouthChinaMorningPost/live', 'asia', 'news', 20, ['hongkong', 'china']),
    S('rthk', 'RTHK', 'https://www.youtube.com/@rthk/live', 'asia', 'public', 180, ['hongkong', 'public']),
    S('ndtv', 'NDTV', 'https://www.youtube.com/@NDTV/live', 'asia', 'news', 0, ['india']),
    S('indiatoday', 'India Today', 'https://www.youtube.com/@IndiaToday/live', 'asia', 'news', 10, ['india']),
    S('wion', 'WION', 'https://www.youtube.com/@WIONews/live', 'asia', 'news', 200, ['india', 'world']),
    S('timesnow', 'Times Now', 'https://www.youtube.com/@TimesNow/live', 'asia', 'news', 25, ['india']),
    S('republic', 'Republic World', 'https://www.youtube.com/@RepublicWorld/live', 'asia', 'news', 15, ['india']),
    S('geo', 'Geo News', 'https://www.youtube.com/@geonews/live', 'asia', 'news', 140, ['pakistan']),
    S('ary', 'ARY News', 'https://www.youtube.com/@ARYNewsTV/live', 'asia', 'news', 30, ['pakistan']),
    S('dawn', 'Dawn News', 'https://www.youtube.com/@DawnNews/live', 'asia', 'news', 200, ['pakistan']),
    S('abs', 'ABS-CBN News', 'https://www.youtube.com/@ABSCBNNews/live', 'asia', 'news', 350, ['philippines']),
    S('gma', 'GMA Integrated News', 'https://www.youtube.com/@gmanews/live', 'asia', 'news', 40, ['philippines']),
    S('kompas', 'Kompas TV', 'https://www.youtube.com/@KompasTV/live', 'asia', 'news', 200, ['indonesia']),
    S('metro', 'Metro TV', 'https://www.youtube.com/@metrotv/live', 'asia', 'news', 210, ['indonesia']),
    S('thaipbs', 'Thai PBS', 'https://www.youtube.com/@ThaiPBS/live', 'asia', 'public', 160, ['thailand', 'public']),
    S('vietv', 'VTV24', 'https://www.youtube.com/@vtv24/live', 'asia', 'news', 200, ['vietnam']),
    S('cgtn', 'CGTN', 'https://www.youtube.com/@cgtn/live', 'asia', 'news', 0, ['china', 'world']),
    S('cctv', 'CCTV+', 'https://www.youtube.com/@CCTV/live', 'asia', 'news', 5, ['china']),

    // ── Oceania ──
    S('abcau', 'ABC News AU', 'https://www.youtube.com/@abcnewsaustralia/live', 'oceania', 'news', 200, ['australia', 'public']),
    S('sbs', 'SBS News', 'https://www.youtube.com/@SBSNews/live', 'oceania', 'news', 210, ['australia']),
    S('skyau', 'Sky News Australia', 'https://www.youtube.com/@SkyNewsAustralia/live', 'oceania', 'news', 195, ['australia']),
    S('7news', '7NEWS Australia', 'https://www.youtube.com/@7newsAustralia/live', 'oceania', 'news', 30, ['australia']),
    S('9news', '9 News Australia', 'https://www.youtube.com/@9NewsAustralia/live', 'oceania', 'news', 40, ['australia']),
    S('tvnz', '1News NZ', 'https://www.youtube.com/@1NewsNZ/live', 'oceania', 'news', 200, ['newzealand']),
    S('newshub', 'Newshub', 'https://www.youtube.com/@NewshubNZ/live', 'oceania', 'news', 210, ['newzealand']),

    // ── World / wire ──
    S('reu', 'Reuters', 'https://www.youtube.com/@Reuters/live', 'world', 'news', 15, ['wire', 'breaking']),
    S('afp', 'AFP News Agency', 'https://www.youtube.com/@AFPnewsagency/live', 'world', 'news', 20, ['wire']),
    S('un', 'United Nations', 'https://www.youtube.com/@UN/live', 'world', 'public', 200, ['world', 'public', 'politics']),
    S('who', 'World Health Org', 'https://www.youtube.com/@WHO/live', 'world', 'public', 180, ['health', 'public']),
    S('nato', 'NATO', 'https://www.youtube.com/@NATO/live', 'world', 'public', 220, ['politics', 'public']),

    // ── Weather (global) ──
    S('weatherch', 'The Weather Channel', 'https://www.youtube.com/@TheWeatherChannel/live', 'weather', 'weather', 200, ['weather', 'us']),
    S('accu', 'AccuWeather', 'https://www.youtube.com/@AccuWeather/live', 'weather', 'weather', 210, ['weather']),
    S('weathernation', 'WeatherNation', 'https://www.youtube.com/@WeatherNation/live', 'weather', 'weather', 195, ['weather', 'us']),
    S('foxweather', 'FOX Weather', 'https://www.youtube.com/@FOXWeather/live', 'weather', 'weather', 25, ['weather', 'us']),
    S('bbcweather', 'BBC Weather', 'https://www.youtube.com/@BBCWeather/live', 'weather', 'weather', 200, ['weather', 'uk']),
    S('metoffice', 'Met Office', 'https://www.youtube.com/@metoffice/live', 'weather', 'weather', 205, ['weather', 'uk', 'public']),
    S('weatherca', 'The Weather Network', 'https://www.youtube.com/@TheWeatherNetwork/live', 'weather', 'weather', 190, ['weather', 'canada']),
    S('bom', 'Bureau of Meteorology', 'https://www.youtube.com/@BureauOfMeteorology/live', 'weather', 'weather', 180, ['weather', 'australia', 'public']),
    S('jma', 'JMA / Japan Weather', 'https://www.youtube.com/@JMA_kishou/live', 'weather', 'weather', 330, ['weather', 'japan', 'public']),
    S('kma', 'KMA Weather', 'https://www.youtube.com/@kma_gov/live', 'weather', 'weather', 210, ['weather', 'korea', 'public']),
    S('noaa', 'NOAA', 'https://www.youtube.com/@NOAA/live', 'weather', 'weather', 200, ['weather', 'us', 'public']),
    S('nhc', 'National Hurricane Center', 'https://www.youtube.com/@NWSNHC/live', 'weather', 'weather', 15, ['weather', 'us', 'public', 'storm']),
    S('spc', 'Storm Prediction Center', 'https://www.youtube.com/@NWSSPC/live', 'weather', 'weather', 20, ['weather', 'us', 'public', 'storm']),
    S('weatherau', 'Weatherzone', 'https://www.youtube.com/@weatherzone/live', 'weather', 'weather', 175, ['weather', 'australia']),
    S('meteonews', 'MeteoNews', 'https://www.youtube.com/@meteonews/live', 'weather', 'weather', 185, ['weather', 'europe']),
    S('wetter', 'wetter.com', 'https://www.youtube.com/@wettercom/live', 'weather', 'weather', 190, ['weather', 'germany']),
    S('meteofr', 'Météo-France', 'https://www.youtube.com/@meteofrance/live', 'weather', 'weather', 200, ['weather', 'france', 'public']),

    // ── Public access / PEG / government (US + intl) ──
    S('nyctv', 'NYC Media / NYC TV', 'https://www.youtube.com/@NYCMedia/live', 'public', 'public', 260, ['public', 'us', 'local']),
    S('lacounty', 'LA County', 'https://www.youtube.com/@LACounty/live', 'public', 'public', 250, ['public', 'us', 'local']),
    S('cityla', 'City of Los Angeles', 'https://www.youtube.com/@lacity/live', 'public', 'public', 255, ['public', 'us', 'local']),
    S('sfgov', 'SFGovTV', 'https://www.youtube.com/@SFGovTV/live', 'public', 'public', 240, ['public', 'us', 'local']),
    S('chi', 'Chicago City Clerk', 'https://www.youtube.com/@ChicagoCityClerk/live', 'public', 'public', 230, ['public', 'us', 'local']),
    S('boston', 'City of Boston', 'https://www.youtube.com/@CityofBoston/live', 'public', 'public', 220, ['public', 'us', 'local']),
    S('seattle', 'Seattle Channel', 'https://www.youtube.com/@seattlechannel/live', 'public', 'public', 210, ['public', 'us', 'local']),
    S('austin', 'ATXN / Austin', 'https://www.youtube.com/@ATXN/live', 'public', 'public', 200, ['public', 'us', 'local']),
    S('houston', 'Houston Public Media', 'https://www.youtube.com/@HoustonPublicMedia/live', 'public', 'public', 190, ['public', 'us', 'local']),
    S('philly', 'PhillyCAM / City', 'https://www.youtube.com/@PhillyCAM/live', 'public', 'public', 180, ['public', 'us', 'local']),
    S('dc', 'DC Council', 'https://www.youtube.com/@DCCouncil/live', 'public', 'public', 270, ['public', 'us', 'local', 'politics']),
    S('house', 'US House Live', 'https://www.youtube.com/@HouseofRepresentatives/live', 'public', 'public', 265, ['public', 'us', 'politics']),
    S('senate', 'US Senate', 'https://www.youtube.com/@USSenate/live', 'public', 'public', 268, ['public', 'us', 'politics']),
    S('whitehouse', 'The White House', 'https://www.youtube.com/@whitehouse/live', 'public', 'public', 200, ['public', 'us', 'politics']),
    S('nasa', 'NASA Live', 'https://www.youtube.com/@NASA/live', 'public', 'public', 220, ['public', 'science', 'space']),
    S('nasatv', 'NASA TV Public', 'https://www.youtube.com/@NASAtelevision/live', 'public', 'public', 225, ['public', 'science', 'space']),
    S('esa', 'ESA', 'https://www.youtube.com/@EuropeanSpaceAgency/live', 'public', 'public', 210, ['public', 'science', 'space']),
    S('congressgov', 'Congress.gov', 'https://www.youtube.com/@Congressdotgov/live', 'public', 'public', 250, ['public', 'us', 'politics']),
    S('supremecourt', 'US Supreme Court', 'https://www.youtube.com/@USSupremeCourt/live', 'public', 'public', 240, ['public', 'us', 'politics']),
    S('govcan', 'Government of Canada', 'https://www.youtube.com/@gcCanada/live', 'public', 'public', 200, ['public', 'canada']),
    S('parlca', 'Parliament of Canada', 'https://www.youtube.com/@ParlofCanada/live', 'public', 'public', 205, ['public', 'canada', 'politics']),
    S('ukgov', 'UK Government', 'https://www.youtube.com/@ukgov/live', 'public', 'public', 195, ['public', 'uk']),
    S('number10', 'UK Prime Minister', 'https://www.youtube.com/@Number10gov/live', 'public', 'public', 190, ['public', 'uk', 'politics']),
    S('bundestag', 'Deutscher Bundestag', 'https://www.youtube.com/@bundestag/live', 'public', 'public', 230, ['public', 'germany', 'politics']),
    S('assemblee', 'Assemblée nationale', 'https://www.youtube.com/@AssembleeNationale/live', 'public', 'public', 215, ['public', 'france', 'politics']),
    S('congreso', 'Congreso de los Diputados', 'https://www.youtube.com/@CongresoDiputados/live', 'public', 'public', 10, ['public', 'spain', 'politics']),

    // ══════════════════════════════════════════════════════════
    // EarthCam-style · monuments · highways · cityscapes · nature
    // Prefer YouTube /live pages (yt-dlp). Many cams rotate offline.
    // ══════════════════════════════════════════════════════════

    // ── EarthCam network / hubs ──
    S('earthcam', 'EarthCam Live', 'https://www.youtube.com/@EarthCam/live', 'earthcam', 'earthcam', 40, ['earthcam', 'landmark', 'city']),
    S('earthcamyt', 'EarthCam Network', 'https://www.youtube.com/@earthcamdotcom/live', 'earthcam', 'earthcam', 45, ['earthcam', 'landmark']),
    S('skylinewebcams', 'SkylineWebcams', 'https://www.youtube.com/@SkylineWebcams/live', 'earthcam', 'earthcam', 200, ['earthcam', 'landmark', 'city']),
    S('webcamtaxi', 'Webcam Taxi', 'https://www.youtube.com/@WebcamTaxi/live', 'earthcam', 'earthcam', 190, ['earthcam', 'city']),
    S('liveworldcams', 'Live World Cams', 'https://www.youtube.com/@LiveWorldCams/live', 'earthcam', 'earthcam', 180, ['earthcam', 'city']),

    // ── USA · monuments & icons ──
    S('ec-timessq', 'Times Square · NYC', 'https://www.youtube.com/@EarthCam/live', 'earthcam-us', 'earthcam', 10, ['earthcam', 'monument', 'nyc', 'us']),
    S('ec-brooklyn', 'Brooklyn Bridge · NYC', 'https://www.youtube.com/results?search_query=brooklyn+bridge+live+cam', 'earthcam-us', 'earthcam', 200, ['earthcam', 'monument', 'nyc', 'us']),
    S('ec-statue', 'Statue of Liberty · NYC', 'https://www.youtube.com/results?search_query=statue+of+liberty+live+cam', 'earthcam-us', 'earthcam', 210, ['earthcam', 'monument', 'nyc', 'us']),
    S('ec-empire', 'Empire State Building', 'https://www.youtube.com/results?search_query=empire+state+building+live+cam', 'earthcam-us', 'earthcam', 220, ['earthcam', 'monument', 'nyc', 'us']),
    S('ec-centralpark', 'Central Park · NYC', 'https://www.youtube.com/results?search_query=central+park+live+cam', 'earthcam-us', 'earthcam', 140, ['earthcam', 'nature', 'nyc', 'us']),
    S('ec-whouse', 'White House area · DC', 'https://www.youtube.com/results?search_query=white+house+live+cam', 'earthcam-us', 'earthcam', 200, ['earthcam', 'monument', 'dc', 'us']),
    S('ec-natmall', 'National Mall · DC', 'https://www.youtube.com/results?search_query=national+mall+live+cam', 'earthcam-us', 'earthcam', 205, ['earthcam', 'monument', 'dc', 'us']),
    S('ec-ggbridge', 'Golden Gate Bridge', 'https://www.youtube.com/results?search_query=golden+gate+bridge+live+cam', 'earthcam-us', 'earthcam', 195, ['earthcam', 'monument', 'sf', 'us']),
    S('ec-sf', 'San Francisco Skyline', 'https://www.youtube.com/results?search_query=san+francisco+live+cam', 'earthcam-us', 'earthcam', 200, ['earthcam', 'city', 'sf', 'us']),
    S('ec-hollywood', 'Hollywood Sign · LA', 'https://www.youtube.com/results?search_query=hollywood+sign+live+cam', 'earthcam-us', 'earthcam', 30, ['earthcam', 'monument', 'la', 'us']),
    S('ec-santamonica', 'Santa Monica Pier', 'https://www.youtube.com/results?search_query=santa+monica+pier+live+cam', 'earthcam-us', 'earthcam', 200, ['earthcam', 'landmark', 'la', 'us']),
    S('ec-vegas', 'Las Vegas Strip', 'https://www.youtube.com/results?search_query=las+vegas+strip+live+cam', 'earthcam-us', 'earthcam', 280, ['earthcam', 'city', 'vegas', 'us']),
    S('ec-bellagio', 'Bellagio Fountains · Vegas', 'https://www.youtube.com/results?search_query=bellagio+fountains+live+cam', 'earthcam-us', 'earthcam', 290, ['earthcam', 'landmark', 'vegas', 'us']),
    S('ec-miami', 'Miami Beach / Ocean Drive', 'https://www.youtube.com/results?search_query=miami+beach+live+cam', 'earthcam-us', 'earthcam', 190, ['earthcam', 'city', 'miami', 'us']),
    S('ec-chicago', 'Chicago Bean / Millennium', 'https://www.youtube.com/results?search_query=chicago+live+cam+skyline', 'earthcam-us', 'earthcam', 210, ['earthcam', 'city', 'chicago', 'us']),
    S('ec-seattle', 'Seattle Space Needle', 'https://www.youtube.com/results?search_query=space+needle+live+cam', 'earthcam-us', 'earthcam', 200, ['earthcam', 'monument', 'seattle', 'us']),
    S('ec-nola', 'New Orleans French Quarter', 'https://www.youtube.com/results?search_query=french+quarter+live+cam', 'earthcam-us', 'earthcam', 40, ['earthcam', 'city', 'nola', 'us']),
    S('ec-boston', 'Boston Harbor', 'https://www.youtube.com/results?search_query=boston+harbor+live+cam', 'earthcam-us', 'earthcam', 200, ['earthcam', 'city', 'boston', 'us']),
    S('ec-niagara', 'Niagara Falls', 'https://www.youtube.com/results?search_query=niagara+falls+live+cam', 'earthcam-us', 'earthcam', 195, ['earthcam', 'nature', 'monument', 'us']),
    S('ec-grandcanyon', 'Grand Canyon', 'https://www.youtube.com/results?search_query=grand+canyon+live+cam', 'earthcam-us', 'earthcam', 30, ['earthcam', 'nature', 'monument', 'us']),
    S('ec-yosemite', 'Yosemite Valley', 'https://www.youtube.com/results?search_query=yosemite+live+cam', 'earthcam-us', 'earthcam', 140, ['earthcam', 'nature', 'us']),
    S('ec-yellowstone', 'Yellowstone', 'https://www.youtube.com/results?search_query=yellowstone+live+cam', 'earthcam-us', 'earthcam', 120, ['earthcam', 'nature', 'us']),
    S('ec-hawaii', 'Waikiki Beach · Hawaii', 'https://www.youtube.com/results?search_query=waikiki+live+cam', 'earthcam-us', 'earthcam', 200, ['earthcam', 'nature', 'hawaii', 'us']),
    S('ec-disney', 'Disney parks live', 'https://www.youtube.com/results?search_query=disney+world+live+cam', 'earthcam-us', 'earthcam', 280, ['earthcam', 'landmark', 'us']),

    // ── USA · highways / traffic (DOT / city cams) ──
    S('ec-traffic-nyc', 'NYC Traffic cams', 'https://www.youtube.com/results?search_query=nyc+traffic+live+cam', 'earthcam-highway', 'earthcam', 20, ['earthcam', 'highway', 'nyc', 'us']),
    S('ec-traffic-la', 'LA Freeways', 'https://www.youtube.com/results?search_query=los+angeles+freeway+live+cam', 'earthcam-highway', 'earthcam', 25, ['earthcam', 'highway', 'la', 'us']),
    S('ec-traffic-sf', 'Bay Area Traffic', 'https://www.youtube.com/results?search_query=bay+area+traffic+live+cam', 'earthcam-highway', 'earthcam', 200, ['earthcam', 'highway', 'sf', 'us']),
    S('ec-traffic-chi', 'Chicago Traffic', 'https://www.youtube.com/results?search_query=chicago+traffic+live+cam', 'earthcam-highway', 'earthcam', 210, ['earthcam', 'highway', 'chicago', 'us']),
    S('ec-traffic-atl', 'Atlanta Highways', 'https://www.youtube.com/results?search_query=atlanta+traffic+live+cam', 'earthcam-highway', 'earthcam', 15, ['earthcam', 'highway', 'us']),
    S('ec-traffic-dfw', 'DFW Highways', 'https://www.youtube.com/results?search_query=dallas+traffic+live+cam', 'earthcam-highway', 'earthcam', 30, ['earthcam', 'highway', 'us']),
    S('ec-traffic-hou', 'Houston Freeways', 'https://www.youtube.com/results?search_query=houston+traffic+live+cam', 'earthcam-highway', 'earthcam', 35, ['earthcam', 'highway', 'us']),
    S('ec-traffic-sea', 'Seattle Traffic', 'https://www.youtube.com/results?search_query=seattle+traffic+live+cam', 'earthcam-highway', 'earthcam', 200, ['earthcam', 'highway', 'us']),
    S('ec-traffic-den', 'Denver I-25 / I-70', 'https://www.youtube.com/results?search_query=denver+traffic+live+cam', 'earthcam-highway', 'earthcam', 220, ['earthcam', 'highway', 'us']),
    S('ec-traffic-phx', 'Phoenix Freeways', 'https://www.youtube.com/results?search_query=phoenix+traffic+live+cam', 'earthcam-highway', 'earthcam', 40, ['earthcam', 'highway', 'us']),
    S('ec-i95', 'I-95 corridor cams', 'https://www.youtube.com/results?search_query=I-95+traffic+live+cam', 'earthcam-highway', 'earthcam', 10, ['earthcam', 'highway', 'us']),
    S('ec-i405', 'I-405 · SoCal', 'https://www.youtube.com/results?search_query=I-405+traffic+live+cam', 'earthcam-highway', 'earthcam', 25, ['earthcam', 'highway', 'us']),
    S('ec-i5', 'I-5 West Coast', 'https://www.youtube.com/results?search_query=I-5+traffic+live+cam', 'earthcam-highway', 'earthcam', 200, ['earthcam', 'highway', 'us']),
    S('ec-gwb', 'George Washington Bridge', 'https://www.youtube.com/results?search_query=george+washington+bridge+live+cam', 'earthcam-highway', 'earthcam', 205, ['earthcam', 'highway', 'monument', 'nyc', 'us']),
    S('ec-holland', 'Holland Tunnel / NJ', 'https://www.youtube.com/results?search_query=holland+tunnel+traffic+live', 'earthcam-highway', 'earthcam', 210, ['earthcam', 'highway', 'nyc', 'us']),
    S('ec-airport-jfk', 'JFK Airport area', 'https://www.youtube.com/results?search_query=JFK+airport+live+cam', 'earthcam-highway', 'earthcam', 220, ['earthcam', 'airport', 'nyc', 'us']),
    S('ec-airport-lax', 'LAX area cams', 'https://www.youtube.com/results?search_query=LAX+live+cam', 'earthcam-highway', 'earthcam', 30, ['earthcam', 'airport', 'la', 'us']),
    S('ec-airport-ord', 'O\'Hare area', 'https://www.youtube.com/results?search_query=ohare+airport+live+cam', 'earthcam-highway', 'earthcam', 210, ['earthcam', 'airport', 'chicago', 'us']),

    // ── Europe · monuments ──
    S('ec-eiffel', 'Eiffel Tower · Paris', 'https://www.youtube.com/results?search_query=eiffel+tower+live+cam', 'earthcam-eu', 'earthcam', 210, ['earthcam', 'monument', 'paris', 'eu']),
    S('ec-louvre', 'Louvre / Louvre Pyramid', 'https://www.youtube.com/results?search_query=louvre+live+cam', 'earthcam-eu', 'earthcam', 215, ['earthcam', 'monument', 'paris', 'eu']),
    S('ec-notredame', 'Notre-Dame · Paris', 'https://www.youtube.com/results?search_query=notre+dame+paris+live+cam', 'earthcam-eu', 'earthcam', 220, ['earthcam', 'monument', 'paris', 'eu']),
    S('ec-arc', 'Arc de Triomphe', 'https://www.youtube.com/results?search_query=arc+de+triomphe+live+cam', 'earthcam-eu', 'earthcam', 200, ['earthcam', 'monument', 'paris', 'eu']),
    S('ec-london', 'London Eye / Thames', 'https://www.youtube.com/results?search_query=london+eye+live+cam', 'earthcam-eu', 'earthcam', 200, ['earthcam', 'monument', 'london', 'uk']),
    S('ec-bigben', 'Big Ben / Parliament', 'https://www.youtube.com/results?search_query=big+ben+live+cam', 'earthcam-eu', 'earthcam', 205, ['earthcam', 'monument', 'london', 'uk']),
    S('ec-towerbridge', 'Tower Bridge · London', 'https://www.youtube.com/results?search_query=tower+bridge+live+cam', 'earthcam-eu', 'earthcam', 210, ['earthcam', 'monument', 'london', 'uk']),
    S('ec-piccadilly', 'Piccadilly Circus', 'https://www.youtube.com/results?search_query=piccadilly+circus+live+cam', 'earthcam-eu', 'earthcam', 15, ['earthcam', 'city', 'london', 'uk']),
    S('ec-abbeyroad', 'Abbey Road Crossing', 'https://www.youtube.com/results?search_query=abbey+road+live+cam', 'earthcam-eu', 'earthcam', 40, ['earthcam', 'landmark', 'london', 'uk']),
    S('ec-colosseum', 'Colosseum · Rome', 'https://www.youtube.com/results?search_query=colosseum+live+cam', 'earthcam-eu', 'earthcam', 30, ['earthcam', 'monument', 'rome', 'eu']),
    S('ec-vatican', 'St. Peter\'s Square', 'https://www.youtube.com/results?search_query=st+peters+square+live+cam', 'earthcam-eu', 'earthcam', 220, ['earthcam', 'monument', 'vatican', 'eu']),
    S('ec-venice', 'Venice Grand Canal', 'https://www.youtube.com/results?search_query=venice+grand+canal+live+cam', 'earthcam-eu', 'earthcam', 200, ['earthcam', 'city', 'venice', 'eu']),
    S('ec-duomo', 'Duomo · Florence / Milan', 'https://www.youtube.com/results?search_query=duomo+live+cam', 'earthcam-eu', 'earthcam', 210, ['earthcam', 'monument', 'italy', 'eu']),
    S('ec-sagrada', 'Sagrada Família · Barcelona', 'https://www.youtube.com/results?search_query=sagrada+familia+live+cam', 'earthcam-eu', 'earthcam', 15, ['earthcam', 'monument', 'barcelona', 'eu']),
    S('ec-ramblas', 'Las Ramblas · Barcelona', 'https://www.youtube.com/results?search_query=las+ramblas+live+cam', 'earthcam-eu', 'earthcam', 20, ['earthcam', 'city', 'barcelona', 'eu']),
    S('ec-acropolis', 'Acropolis · Athens', 'https://www.youtube.com/results?search_query=acropolis+live+cam', 'earthcam-eu', 'earthcam', 40, ['earthcam', 'monument', 'athens', 'eu']),
    S('ec-brandenburg', 'Brandenburg Gate · Berlin', 'https://www.youtube.com/results?search_query=brandenburg+gate+live+cam', 'earthcam-eu', 'earthcam', 220, ['earthcam', 'monument', 'berlin', 'eu']),
    S('ec-neuschwanstein', 'Neuschwanstein Castle', 'https://www.youtube.com/results?search_query=neuschwanstein+live+cam', 'earthcam-eu', 'earthcam', 200, ['earthcam', 'monument', 'germany', 'eu']),
    S('ec-prague', 'Prague Old Town / Astronomical Clock', 'https://www.youtube.com/results?search_query=prague+astronomical+clock+live+cam', 'earthcam-eu', 'earthcam', 30, ['earthcam', 'monument', 'prague', 'eu']),
    S('ec-amsterdam', 'Amsterdam Canals', 'https://www.youtube.com/results?search_query=amsterdam+canals+live+cam', 'earthcam-eu', 'earthcam', 200, ['earthcam', 'city', 'amsterdam', 'eu']),
    S('ec-vienna', 'Vienna Stephansplatz', 'https://www.youtube.com/results?search_query=vienna+stephansplatz+live+cam', 'earthcam-eu', 'earthcam', 210, ['earthcam', 'city', 'vienna', 'eu']),
    S('ec-moscow', 'Red Square · Moscow', 'https://www.youtube.com/results?search_query=red+square+live+cam', 'earthcam-eu', 'earthcam', 0, ['earthcam', 'monument', 'moscow']),
    S('ec-istanbul', 'Hagia Sophia / Bosphorus', 'https://www.youtube.com/results?search_query=hagia+sophia+live+cam', 'earthcam-eu', 'earthcam', 40, ['earthcam', 'monument', 'istanbul']),
    S('ec-m25', 'London M25 traffic', 'https://www.youtube.com/results?search_query=M25+traffic+live+cam', 'earthcam-highway', 'earthcam', 200, ['earthcam', 'highway', 'london', 'uk']),
    S('ec-peripherique', 'Paris Périphérique', 'https://www.youtube.com/results?search_query=peripherique+paris+live+cam', 'earthcam-highway', 'earthcam', 210, ['earthcam', 'highway', 'paris', 'eu']),

    // ── Asia · monuments & cityscapes ──
    S('ec-shibuya', 'Shibuya Crossing · Tokyo', 'https://www.youtube.com/results?search_query=shibuya+crossing+live+cam', 'earthcam-asia', 'earthcam', 330, ['earthcam', 'city', 'tokyo', 'japan']),
    S('ec-shinjuku', 'Shinjuku · Tokyo', 'https://www.youtube.com/results?search_query=shinjuku+live+cam', 'earthcam-asia', 'earthcam', 335, ['earthcam', 'city', 'tokyo', 'japan']),
    S('ec-tokyo-tower', 'Tokyo Tower / Skytree', 'https://www.youtube.com/results?search_query=tokyo+skytree+live+cam', 'earthcam-asia', 'earthcam', 340, ['earthcam', 'monument', 'tokyo', 'japan']),
    S('ec-fuji', 'Mount Fuji', 'https://www.youtube.com/results?search_query=mount+fuji+live+cam', 'earthcam-asia', 'earthcam', 200, ['earthcam', 'nature', 'monument', 'japan']),
    S('ec-kyoto', 'Fushimi Inari / Kyoto', 'https://www.youtube.com/results?search_query=fushimi+inari+live+cam', 'earthcam-asia', 'earthcam', 20, ['earthcam', 'monument', 'kyoto', 'japan']),
    S('ec-seoul', 'Myeongdong / Seoul', 'https://www.youtube.com/results?search_query=seoul+live+cam', 'earthcam-asia', 'earthcam', 210, ['earthcam', 'city', 'seoul', 'korea']),
    S('ec-hongkong', 'Hong Kong Victoria Harbour', 'https://www.youtube.com/results?search_query=victoria+harbour+live+cam', 'earthcam-asia', 'earthcam', 200, ['earthcam', 'city', 'hongkong']),
    S('ec-shanghai', 'Shanghai Bund', 'https://www.youtube.com/results?search_query=shanghai+bund+live+cam', 'earthcam-asia', 'earthcam', 15, ['earthcam', 'city', 'shanghai', 'china']),
    S('ec-greatwall', 'Great Wall of China', 'https://www.youtube.com/results?search_query=great+wall+live+cam', 'earthcam-asia', 'earthcam', 20, ['earthcam', 'monument', 'china']),
    S('ec-taj', 'Taj Mahal', 'https://www.youtube.com/results?search_query=taj+mahal+live+cam', 'earthcam-asia', 'earthcam', 40, ['earthcam', 'monument', 'india']),
    S('ec-dubai', 'Burj Khalifa / Dubai Marina', 'https://www.youtube.com/results?search_query=burj+khalifa+live+cam', 'earthcam-asia', 'earthcam', 200, ['earthcam', 'monument', 'dubai', 'me']),
    S('ec-singapore', 'Marina Bay Sands', 'https://www.youtube.com/results?search_query=marina+bay+sands+live+cam', 'earthcam-asia', 'earthcam', 190, ['earthcam', 'monument', 'singapore']),
    S('ec-bangkok', 'Bangkok Skyline', 'https://www.youtube.com/results?search_query=bangkok+live+cam', 'earthcam-asia', 'earthcam', 40, ['earthcam', 'city', 'bangkok']),
    S('ec-sydney', 'Sydney Opera House / Harbour', 'https://www.youtube.com/results?search_query=sydney+opera+house+live+cam', 'earthcam-asia', 'earthcam', 200, ['earthcam', 'monument', 'sydney', 'oceania']),
    S('ec-harbourbridge', 'Sydney Harbour Bridge', 'https://www.youtube.com/results?search_query=sydney+harbour+bridge+live+cam', 'earthcam-asia', 'earthcam', 205, ['earthcam', 'monument', 'sydney', 'oceania']),

    // ── LatAm · Africa · Middle East · world icons ──
    S('ec-riodejaneiro', 'Christ the Redeemer / Rio', 'https://www.youtube.com/results?search_query=christ+the+redeemer+live+cam', 'earthcam-world', 'earthcam', 120, ['earthcam', 'monument', 'rio', 'latam']),
    S('ec-copacabana', 'Copacabana Beach', 'https://www.youtube.com/results?search_query=copacabana+live+cam', 'earthcam-world', 'earthcam', 200, ['earthcam', 'nature', 'rio', 'latam']),
    S('ec-machupicchu', 'Machu Picchu', 'https://www.youtube.com/results?search_query=machu+picchu+live+cam', 'earthcam-world', 'earthcam', 100, ['earthcam', 'monument', 'peru', 'latam']),
    S('ec-mexico', 'Zócalo · Mexico City', 'https://www.youtube.com/results?search_query=zocalo+mexico+live+cam', 'earthcam-world', 'earthcam', 140, ['earthcam', 'city', 'mexico', 'latam']),
    S('ec-pyramids', 'Giza Pyramids', 'https://www.youtube.com/results?search_query=giza+pyramids+live+cam', 'earthcam-world', 'earthcam', 40, ['earthcam', 'monument', 'egypt', 'africa']),
    S('ec-tablemt', 'Table Mountain · Cape Town', 'https://www.youtube.com/results?search_query=table+mountain+live+cam', 'earthcam-world', 'earthcam', 200, ['earthcam', 'nature', 'southafrica', 'africa']),
    S('ec-jerusalem', 'Western Wall / Jerusalem', 'https://www.youtube.com/results?search_query=western+wall+live+cam', 'earthcam-world', 'earthcam', 45, ['earthcam', 'monument', 'jerusalem', 'me']),
    S('ec-petra', 'Petra', 'https://www.youtube.com/results?search_query=petra+jordan+live+cam', 'earthcam-world', 'earthcam', 30, ['earthcam', 'monument', 'jordan', 'me']),
    S('ec-antarctica', 'Antarctica research cams', 'https://www.youtube.com/results?search_query=antarctica+live+cam', 'earthcam-world', 'earthcam', 200, ['earthcam', 'nature', 'world']),
    S('ec-northernlights', 'Aurora / Northern Lights', 'https://www.youtube.com/results?search_query=northern+lights+live+cam', 'earthcam-world', 'earthcam', 260, ['earthcam', 'nature', 'world']),
    S('ec-volcano', 'Volcano live cams', 'https://www.youtube.com/results?search_query=volcano+live+cam', 'earthcam-world', 'earthcam', 10, ['earthcam', 'nature', 'world']),
    S('ec-iss', 'ISS / Earth from space', 'https://www.youtube.com/@NASA/live', 'earthcam-world', 'earthcam', 220, ['earthcam', 'science', 'space', 'world']),
  ];

  const REGIONS = [
    { id: 'us', label: 'United States · News', kinds: ['news'] },
    { id: 'ca', label: 'Canada', kinds: ['news', 'public'] },
    { id: 'latam', label: 'Latin America', kinds: ['news'] },
    { id: 'uk', label: 'UK & Ireland', kinds: ['news', 'public'] },
    { id: 'eu', label: 'Europe', kinds: ['news', 'public'] },
    { id: 'me', label: 'Middle East', kinds: ['news'] },
    { id: 'africa', label: 'Africa', kinds: ['news'] },
    { id: 'asia', label: 'Asia', kinds: ['news', 'public'] },
    { id: 'oceania', label: 'Oceania', kinds: ['news'] },
    { id: 'world', label: 'World · Wire · Multilateral', kinds: ['news', 'public'] },
    { id: 'weather', label: 'Weather · All regions', kinds: ['weather'] },
    { id: 'public', label: 'Public access · PEG · Government', kinds: ['public'] },
    { id: 'earthcam', label: 'EarthCam · Network hubs', kinds: ['earthcam'] },
    { id: 'earthcam-us', label: 'EarthCam · US monuments & cities', kinds: ['earthcam'] },
    { id: 'earthcam-highway', label: 'EarthCam · Highways · Traffic · Airports', kinds: ['earthcam'] },
    { id: 'earthcam-eu', label: 'EarthCam · Europe monuments', kinds: ['earthcam'] },
    { id: 'earthcam-asia', label: 'EarthCam · Asia · Oceania', kinds: ['earthcam'] },
    { id: 'earthcam-world', label: 'EarthCam · World icons · Nature', kinds: ['earthcam'] },
  ];

  /** Theme buckets for AI caption / transcript clustering. */
  const THEMES = [
    { id: 'breaking', label: 'Breaking', keywords: ['breaking', 'urgent', 'developing', 'just in', 'alert'] },
    { id: 'politics', label: 'Politics', keywords: ['election', 'congress', 'parliament', 'president', 'minister', 'vote', 'senate', 'bill'] },
    { id: 'conflict', label: 'Conflict', keywords: ['war', 'attack', 'military', 'missile', 'ceasefire', 'troops', 'strike'] },
    { id: 'markets', label: 'Markets', keywords: ['stock', 'market', 'fed', 'inflation', 'gdp', 'earnings', 'crypto', 'oil'] },
    { id: 'weather', label: 'Weather', keywords: ['storm', 'hurricane', 'flood', 'snow', 'forecast', 'temperature', 'tornado', 'heat'] },
    { id: 'health', label: 'Health', keywords: ['health', 'virus', 'hospital', 'who', 'vaccine', 'outbreak'] },
    { id: 'science', label: 'Science · Space', keywords: ['nasa', 'space', 'launch', 'science', 'climate', 'research'] },
    { id: 'local', label: 'Local · Public', keywords: ['city', 'council', 'county', 'mayor', 'hearing', 'public access'] },
    { id: 'culture', label: 'Culture', keywords: ['culture', 'sport', 'film', 'music', 'festival'] },
    { id: 'earthcam', label: 'EarthCam · Scenic', keywords: ['cam', 'live cam', 'traffic', 'monument', 'bridge', 'skyline', 'beach', 'highway', 'landmark', 'earthcam'] },
    { id: 'unsorted', label: 'Unsorted', keywords: [] },
  ];

  function byRegion(regionId) {
    return MAJOR_NEWS.filter((s) => s.region === regionId);
  }

  function byKind(kind) {
    return MAJOR_NEWS.filter((s) => s.kind === kind);
  }

  function findById(id) {
    return MAJOR_NEWS.find((s) => s.id === id) || null;
  }

  global.GY_NEWS = {
    MAJOR_NEWS: MAJOR_NEWS,
    REGIONS: REGIONS,
    THEMES: THEMES,
    byRegion: byRegion,
    byKind: byKind,
    findById: findById,
  };
})(typeof window !== 'undefined' ? window : globalThis);
