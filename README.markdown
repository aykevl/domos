# Domotica server

This is the server part of a personal home automation system. Clients can send
data to it (currently only temperature) and control light (currently only an
RGB LED).

See also:

  * [domo-web](https://github.com/aykevl/domo-web) for the web interface, which
    communicates with this backend.
  * [domo-rpi](https://github.com/aykevl/domo-rpi) for the Raspberry Pi side,
    which links to this backend and controls the microcontroller.
  * [domo-avr](https://github.com/aykevl/domo-avr) for the microcontroller code
    (Arduino).
