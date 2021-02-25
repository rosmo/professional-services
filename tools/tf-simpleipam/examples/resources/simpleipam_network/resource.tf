resource "simpleipam_network" "test" {
  name       = "myalloc"
  pool       = "pods"
  pool_index = 1
  netmask    = 24
}
