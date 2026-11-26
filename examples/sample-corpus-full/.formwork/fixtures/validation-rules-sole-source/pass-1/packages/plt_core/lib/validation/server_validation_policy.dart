class ServerValidationPolicy {
  // plt-validation-rule:email
  static final email = RegExp(r"^[^@\s]+@[^@\s]+\.[^@\s]+$");

  // plt-validation-rule:phone
  static final phone = RegExp(r"^\+?[0-9]{7,15}$");
}
