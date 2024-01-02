import sys
import soundfile as sf
from pydub import AudioSegment
import io
import struct
import json
import torch
import numpy as np
from pyannote.audio import Pipeline
pipeline = Pipeline.from_pretrained(
    "pyannote/speaker-diarization-3.1",
    use_auth_token="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")

# send pipeline to GPU (when available)
#import torch
#pipeline.to(torch.device("cuda"))

def process_audio(buffer):
    audio_data = np.frombuffer(buffer, dtype=np.float32)

    diarization = pipeline(dict(
      waveform=torch.from_numpy(audio_data).unsqueeze(0), 
      uri="dummy_uri", 
      sample_rate=16000,
      delta_new=0.57
    ))

    timespans = [
        {"speaker": speaker, "start": segment.start, "end": segment.end}
        for segment, _, speaker in diarization.itertracks(yield_label=True)
    ]

    return json.dumps(timespans)

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