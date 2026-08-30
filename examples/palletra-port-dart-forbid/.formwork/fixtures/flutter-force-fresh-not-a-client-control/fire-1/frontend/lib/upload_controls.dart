class UploadControls {
  bool skipCache = false; // want: flutter-force-fresh-not-a-client-control

  Map<String, Object> toRequest() => {'file_name': 'x'};
}
