class UploadControls {
  final String fileName;

  UploadControls(this.fileName);

  Map<String, Object> toRequest() => {'file_name': fileName};
}
