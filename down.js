import YtDlpWrap from "yt-dlp-wrap";

const ytDlp = new YtDlpWrap();

const url = "https://www.youtube.com/watch?v=mAg7PYYVTOg&list=RDmAg7PYYVTOg";

ytDlp.exec([
  url,
  "--cookies", "C:\\Users\\Suran\\Music\\cookies.txt",
  "--playlist-end", "100",
  "-x",
  "--audio-format", "mp3",
  "-o", "C:\\Users\\Suran\\Music\\%(playlist_index)02d - %(title)s.%(ext)s",
  "--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
  "--extractor-args", "youtube:player_client=android"
])
.on("progress", p => console.log(p.percent, "%"))
.on("ytDlpEvent", e => console.log(e))
.on("error", err => console.error(err))
.on("close", () => console.log("Download complete!"));
