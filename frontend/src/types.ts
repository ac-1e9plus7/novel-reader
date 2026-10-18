export interface Book {
  id: string;
  title: string;
  author: string;
  created: string;
  chapter_count: number;
  glossary_revision: number;
}
export interface Clip {
  id: string;
  segment_id: string;
  position: number;
  text: string;
  asset_id: string;
  duration: number;
}
export interface Segment {
  id: string;
  chapter_id: string;
  position: number;
  paragraph: number;
  start: number;
  end: number;
  text: string;
  translation: string;
  translation_revision: number;
  manual: boolean;
  speech_text: string;
  speech_revision: number;
  clips: Clip[];
}
export interface Chapter {
  id: string;
  book_id: string;
  title: string;
  position: number;
  revision: number;
  raw: string;
  segments: Segment[];
  translated: number;
  total: number;
  audio_ready: number;
}
export interface Job {
  id: string;
  chapter_id: string;
  kind: "translation" | "audio";
  status: string;
  done: number;
  total: number;
  message: string;
  created: string;
  items?: { id: string; position: number; status: string; message: string }[];
}
export interface Progress {
  chapter_id: string;
  segment_id: string;
  clip_id: string;
  seconds: number;
  revision: number;
}
export interface Glossary {
  revision: number;
  terms: { source: string; target: string; reading: string }[];
}
export interface Config {
  translation_ready: boolean;
  tts_ready: boolean;
  model: string;
  tts_provider: string;
  tts_model: string;
  voice: string;
  flac_enabled: boolean;
  batch_chars: number;
}
