/**
 * Global live news / weather / public-access catalog for livenews.html.
 * Mirrors + expands news_wall.go MajorNewsSources / ExtendedNewsSources.
 * kind: news | weather | public
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
