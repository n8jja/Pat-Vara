# Pat-Vara
A VARA transporter for the Pat Winlink Open Source project.

## Status:
**9/3/2021**: Initial code base setup (thanks to Chris Keller @xylo04!)
**9/2/2021**: Added the VARA command set to the repository.

The project is still in early phases.  Coomunication has been established between a terminal and the VARA software modem in both HF and FM versions.  The command set has been analyzed.  The next big step is evaluating what commands are sent in what order and what the payload consists of.  What we know so far is:

- VARA communicates over TCP/IP with commands sent to port 8300 by default and 
- message data is sent to port 8301.  

Still in prgress is determining what a properly formatted message looks like and in what order commands are sent to each port.  Once these bits are worked out, it should be relatively trivial to use an existing PAT transporter to create a new one for VARA.

If anyone has any data they have collected on this, please feel free to either upload it to this repository or email me directly at: jeremy (at) mycallsign.com

Jeremy
N8JJA
