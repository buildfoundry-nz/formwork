class ServerValidationPolicy {
  // plt-validation-rule:email
  static final email = RegExp(r"^.+@.+$");

  // plt-validation-rule:phone
  static final phone = RegExp(r"^\+?[0-9]{7,15}$");
}
