import sys
import soundfile as sf
from pydub import AudioSegment
import io
import struct
import json
import numpy as np
from faster_whisper import WhisperModel
import os

model = WhisperModel(
    os.environ.get("MODEL_REPO", "tiny"),
    device=os.environ.get("MODEL_DEVICE", "cpu"),
    compute_type=os.environ.get("MODEL_COMPUTE_TYPE", "int8"),
    #local_files_only=True,
)

# send pipeline to GPU (when available)
#import torch
#pipeline.to(torch.device("cuda"))

def process_audio(buffer):
    audio_data = np.frombuffer(buffer, dtype=np.float32)

    segments, info = model.transcribe(
        audio_data,
        vad_filter=False,
        beam_size=5,
        word_timestamps=True,
        task="transcribe",
    )
    segments = list(segments)

    resp = dict(
        text="".join([segment.text for segment in segments]),
        source_language=info.language,
        source_language_prob=info.language_probability,
        target_language=info.language,
        duration=info.duration,
        all_language_probs={
            language: prob
            for language, prob in info.all_language_probs
        } if info.all_language_probs else None,
        segments=[
            dict(
                id=segment.id,
                seek=segment.seek,
                start=segment.start,
                end=segment.end,
                text=segment.text,
                temperature=segment.temperature,
                avg_logprob=segment.avg_logprob,
                compression_ratio=segment.compression_ratio,
                no_speech_prob=segment.no_speech_prob,
                words=[
                    dict(
                        start=word.start,
                        end=word.end,
                        word=word.word,
                        prob=word.probability,
                    )
                    for word in segment.words
                ] if segment.words else None,
            )
            for segment in segments
        ],
    )

    return json.dumps(resp)

def read_exact(buffer, size):
    data = bytearray()
    while len(data) < size:
        packet = buffer.read(size - len(data))
        if not packet:
            return None
        data.extend(packet)
    return data

def main():
    while True:
        size_line = sys.stdin.buffer.readline()
        if not size_line:
            break 

        size = int(size_line.decode().strip())
        data = read_exact(sys.stdin.buffer, size)
        if not data or len(data) < size:
            break  

        print(process_audio(data))

if __name__ == "__main__":
    main()