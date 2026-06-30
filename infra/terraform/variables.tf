# Variable stubs for the self-hosted deployment. Provider blocks and concrete
# resources are added in Phase 6 once the target cloud is selected.

variable "domain" {
  type        = string
  description = "Base domain; api.<domain> and turn.<domain> are derived from it."
}

variable "region" {
  type        = string
  description = "Cloud region to deploy into."
}

variable "app_instance_size" {
  type        = string
  description = "VM size for the backend/app host(s)."
  default     = "small"
}

variable "turn_instance_size" {
  type        = string
  description = "VM size for the coturn host (needs a public IP)."
  default     = "small"
}

variable "coturn_relay_port_min" {
  type        = number
  description = "Lower bound of the coturn UDP relay port range (open in the firewall)."
  default     = 49160
}

variable "coturn_relay_port_max" {
  type        = number
  description = "Upper bound of the coturn UDP relay port range."
  default     = 49200
}

# Secrets are supplied at apply time (e.g. via a secret manager or TF_VAR_*),
# never committed.
variable "jwt_secret" {
  type        = string
  description = "HMAC secret for signing JWTs."
  sensitive   = true
}

variable "turn_secret" {
  type        = string
  description = "coturn static-auth-secret, shared with the backend."
  sensitive   = true
}
