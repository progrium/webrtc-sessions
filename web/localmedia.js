class LocalMedia {
  constructor() {
    this.onstreamchange = (stream) => null;
    this.ondevicechange = () => null;
    this.audioDevices = [];
    this.videoDevices = [];
    this.audioSource = undefined;
    this.videoSource = undefined;
    this.stream = undefined;
    this.updateStream();
    this.updateDevices();
    navigator.mediaDevices.addEventListener('devicechange', () => this.updateDevices());
  }

  setAudioSource(deviceId) {
    this.audioSource = deviceId;
    this.updateStream();
  }

  setVideoSource(deviceId) {
    this.videoSource = deviceId;
    this.updateStream();
  }

  shareScreen() {
    this.setVideoSource('screen');
    this.updateStream();
  }

  async updateStream() {
    if (this.videoSource === 'screen') {
      this.stream = await navigator.mediaDevices.getDisplayMedia({
        audio: {deviceId: true},
        video: {deviceId: true},
        systemAudio: 'include',
      });
    } else {
      this.stream = await navigator.mediaDevices.getUserMedia({
        audio: {deviceId: this.audioSource ? {exact: this.audioSource} : true},
        video: {deviceId: this.videoSource ? {exact: this.videoSource} : true}
      });
   }
    if (this.onstreamchange) {
      this.onstreamchange(this.stream);
    }
  }

  async updateDevices() {
    const devices = await navigator.mediaDevices.enumerateDevices();
    this.audioDevices = devices.filter(({kind}) => kind === "audioinput");
    this.videoDevices = devices.filter(({kind}) => kind === "videoinput");
    if (this.ondevicechange) {
      this.ondevicechange();
    }
  }
}