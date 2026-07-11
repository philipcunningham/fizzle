# CASIO DIGITAL SAMPLING KEYBOARD MODEL FZ-1 DATA STRUCTURES

*(For Software Developers)*

**Prepared on:** March 18, 1987
**Originally written by:** T. Sasaki — R&D
**Translated by:** O. Ishiyama — Overseas Division
**CASIO TOKYO**

---

## Table of Contents

1. Outline of "Disk" — 1
   - 1-1. Fundamental Format — 3
   - 1-2. Head Format — 4
   - 1-3. Directory Format — 6
   - 1-4. File Format — 7
   - 1-5. Data Packing — 9
2. Outline of "Parameters" — 14
   - 2-1. Details of Voice — 14
   - 2-2. Details of Bank — 22
   - 2-3. Details of Effect — 25
3. Outline of "Dump" — 27
   - 3-1. Procedures for Dump — 28
   - 3-1-1. Outline of External Port — 29
   - 3-1-2. Details of Remote Code — 31
   - 3-1-3. Details of Open Code — 33
   - 3-1-4. Details of Data Code — 35
   - 3-1-5. Details of External Port Hardware — 36
   - 3-2. Outline of MIDI — 40
   - 3-2-1. Details of Remote MIDI — 42
   - 3-2-2. Details of Open/Close MIDI — 42
4. Optional Software — 45
   - 4-1. Expanded Programs — 46
   - 4-2. ROM Entry — 46
   - 4-3. Work Address — 47

---

## 1. Outline of "Disk"

A floppy disk drive unit is built in the FZ-1 machine for use with high-density type 3.5" micro floppy disks. The disk drive is used for inputting/outputting data and parameters and also for loading expanded software programs.

```
Fundamental Format
    ├── Head Format
    ├── Directory Format
    ├── File Format
    └── Packing Data
```

### 1-1. Fundamental Format

The system program which is installed in the FZ-1 machine has a format created on the basis of the IBM format and the FZ-1 formats a disk:

**Double sided, 80 tracks, 8 sectors/track, 1,024 bytes/sector**

The total capacity comes to 1,310,720 bytes.

```
loc
 0       : head - a sector to be used for system
 1       : dir  - a sector to be used for file names
 2 - 1279: sectors to be used for data
```

A logical sector `loc` whose formula is as shown below is occupied with headers, directories, and data.

```
loc = (16 × track) + (8 × head) + (sector - 1)

track:  0 - 79
head:   0 - 1
sector: 1 - 8
```

### 1-2. Head Format

Head data, consisting of Disk Identification `Disk ID` and Cluster Allocation Table `CAT`, is placed exclusively at the logical sector 0.

```
+----------+
| Disk ID  |
+----------+
|          |
|   CAT    |
|          |
+----------+
```

#### a) Disk ID (128 bytes) — Disk Identification

```
+----------------------------+---+---+---+---+
| Disk Name (12 characters)  | 0 | 0 | 2 | 0 |
+----------------------------+---+---+---+---+
| Password  (12 characters)  | 0 | 0 | 0 | 0 |
+----------------------------+---+---+---+---+
|                                            |
|                  unused                    |
|                                            |
+--------------------------------------------+
```

The Disk ID identifies a disk with the record of a disk name and a password.

#### b) CAT (768 bytes) — Cluster Allocation Table

```
MSB        LSB     MSB        LSB
+----+----+        +-----+-----+
| C7 | C0 |  ...   | C15 | C8  |  ...
+----+----+        +-----+-----+
```

`C loc:` "1" for Used, "0" for Unused.

The CAT indicates a status for entire sectors; the figure "1" denotes entire sectors are already used and "0" denotes not yet used. The correspondence with sectors is the sequence from LSB to MSB, and from Low address to High address. The logical sector `loc` verifies these by the formula: `0 ≤ loc ≤ 1279`.

If a figure 0 is obtained from the formula `CAT[loc/8] & (1<<(loc%8))`, then the entire sectors are not yet used. If it is not 0, then they are already used.

### 1-3. Directory Format

The Directory data `dir` is a sector where records file names, identifications and start logical clusters are recorded and placed exclusively at the logical sector 1.

#### a) Dir data

```
+----------------------------+-----+------+
| File Name (12 characters)  | ext | sloc |
+----------------------------+-----+------+
```

The Dir data consists of 16 bytes per file and a piece of floppy disk can store up to 64 files. The portion of a file is shown above.

#### b) File Name

Consists of 12 characters in the ASCII standard. The blank(s) is filled with SPACE (20h). If the first 1 byte is NULL (00h), then the directory is regarded as blank.

#### c) ext

A figure for 2 bytes indicating the data contents in the file. The lower byte denotes data content in each file, and the higher bytes denotes a file address. The FZ-1 allows you to save/load waveform data uninterruptedly on 2 pieces of floppy disk. The higher byte for `ext` takes 0 as value for the first floppy disk and does 1 for the second disk:

| Lower Byte for ext | Data Content          |
|--------------------|-----------------------|
| 0                  | Full Dump Data        |
| 1                  | Voice Data            |
| 2                  | Bank Data             |
| 3                  | Effect Data           |
| 4                  | Sequence Data         |
| 5                  | Expanded Program Data |

#### d) sloc

Denotes Start Logical Cluster, a logical sector heading the data which upcoming file will contain.

### 1-4. File Format

A file consists of a file head (1 sector) and file contents (N sectors). Each sector starts from the sector designated by `Dir` and the location of each sector is assigned at random over the entire area of a disk by the `dBP` (data Block Pointer) in the file head.

```
+----------------+
|  A File Head   |
+----------------+
|                |
|  File Contents |
|                |
+----------------+
```

#### a) File Head

A file head consists of a dBP area (256 bytes) and a work area (768 bytes). The dBP (data Block Pointer) consists of a `ss` (Start Sector) and `es` (End Sector). These are pointers of 2 bytes each existing in the logical sector; `ss` points the head and `es` points the end of each sector even when file contents are randomly located over the entire area of a disk. There exists 64 dBP's in a dBP area, which is a cluster of dBP's. A file will end if a dBP of ss=es=0 exists or when all the 64 dBP's are consumed over.

```
+------------------+
| (256 bytes)      |  dBP Area
+------------------+
| (768 bytes)      |
|                  |  Work Area
|                  |
+------------------+

dBP:
+----+----+
| ss | es |
+----+----+
```

#### b) File Search

The FZ-1 searches a file in the following method:

1. Searches with `Dir` to find a file by the file name and `ext`.
2. Learns from the file `sloc` which sector contains the file head and reads data from the sector ss0 to the sector es0.
3. Reads data from the sector ss1 to the sector es1 by the designation of next dBP.
4. The file ends when the next dBP comes to ss2=es2=0.

### 1-5. Data Packing

All data are structured by 1,024 bytes into a block. The portions which are divided by the block bytes will be explained with charts according to the file data. Files in general are randomly located in a whole disk area; however, the illustration shows files in consecutive locations.

A parameter classification which is used within a block contains nothing but a file field for the dump over External Port and MIDI. Refer to the Outline of Parameters for the detailed contents of Bank, Voice and Effect parameters.

**Chart Legends:**

- a) The big box denotes a block of 1,024-byte data.
- b) The classification inside the box indicates that "nn" bytes are consumed for "xx".
- c) The rectangle without upper line shows that "N" pieces of the same block will follow after the block.
- d) Itemized explanation will be provided with an arrow out of a box if the classified box is too small.

#### b) Full Data — ext=0

```
File Head
+------------------+
| dBP (256 bytes)  |     +----+----+----+
+------------------+     | vn | bn | wn |
| Unused (762 bytes)|    +----+----+----+
+------------------+      |    |    |
                          |    |    └─ wave number (2 bytes)
                          |    └────── bank number (2 bytes)
                          └─────────── voice number (2 bytes)
```

**Bank Data (656 bytes)** — Bank Parameters (a bank/block)

Parameters for a Bank are packed into "bn" pieces of block.
Effect Parameters are located only in the first data block and occupies a 24-byte area after the 960th byte in the block.

**Voice Data (192 bytes)** — Voice Parameters (4 voices/block)

Every 256 bytes of Voice Parameters are packed into a block. The block contains data for 4 voices and occupies (vn+3)/4 pieces of block.

**WaveData (1024 bytes)** — PCM Waveform Data (512 samples/block)

Data for 16-bit waveforms are packed into blocks occupying "wn" pieces of block. One sample consists of 2-byte data and the upper byte is positioned in the higher address.

#### c) Voice Data — ext=1

```
File Head
+------------------+     +---+---+----+
| dBP (256 bytes)  |     | 1 | 0 | wn |
+------------------+     +---+---+----+
                          |   |   |
                          |   |   └─ wave number (2 bytes)
                          |   └───── a constant 0 (2 bytes)
                          └───────── a constant 1 (2 bytes)
```

**VoiceData (192 bytes)** — Parameters for a voice are located.

**WaveData (1024 bytes)** — PCM waveform data (512 samples/block). Sixteen-bit data for waveforms is packed and occupies "wn" pieces of block.

#### d) Bank Data — ext=2

```
File Head
+------------------+     +----+---+----+
| dBP (256 bytes)  |     | vn | 1 | wn |
+------------------+     +----+---+----+
| Unused (762 bytes)|      |   |   |
+------------------+        |   |   └─ wave number (2 bytes)
                            |   └───── a constant 1 (2 bytes)
                            └───────── voice number (2 bytes)
```

**Bank Data (656 bytes)** — One bank amount of Bank Parameters is located in this block.

**Voice Data (192 bytes)** — Voice Parameters (4 voices/block). Every 256 bytes of Voice Parameters are packed into a block. The block contains data for 4 voices and occupies (vn+3)/4 pieces of block.

**WaveData (1024 bytes)** — PCM Waveform Data (512 samples/block). Data for 16-bit waveforms are packed into blocks occupying "wn" pieces of block.

#### e) Effect Data — ext=3

```
File Head
+------------------+     +---+---+---+
| dBP (256 bytes)  |     | 0 | 0 | 0 |
+------------------+     +---+---+---+
                          a constant 0 (each 2 bytes)
```

Effect Parameters occupy a portion of 24 bytes succeeding to the 960th byte in the block.

#### f) Program Data — ext=5

```
File Head
+------------------+     +---+---+----+
| dBP (256 bytes)  |     | 0 | 0 | wn |
+------------------+     +---+---+----+
                          |   |   |
                          |   |   └─ wave number (2 bytes)
                          |   └───── a constant 0
                          └───────── a constant 0
```

**Program Data** — The object code of V50 (8086) is packed into and occupies "wn" pieces of block.

Program Data cannot be transferred over MIDI or External Port.

---

## 2. Outline of "Parameters"

The FZ-1 is provided with 3 different parameters for Voice, Bank and Effect. This chapter will explain the details of each parameter.

```
Parameters
    ├── Voice Parameter
    ├── Bank Parameter
    └── Effect Parameter
```

### 2-1. Details of Voice

Voice data is an assemblage of the parameters which point an address on the PCM encoded waveform memory, an address of the loop, or determines the envelope curve corresponding directly with a PCM-sampled waveform or a synthesized waveform.

The size of parameters is 192 bytes and total data of the voice parameters occupies the area of 12,288 bytes maximum since the FZ-1 internal memory can contain up to 64 voices. In the work area voice parameters start with the label named "voic" to be addressed by every 192 bytes in the sequence from Voice 1 to Voice 64. The content for these 192 bytes is shown in the listing "basic data structure define". It is defined as a structure of "C" of which name is "struct voicedata". The 2-digit hexadecimal numbers existing in the most right end of comment columns are the offset addresses when the header for voicedata is set to 0. The byte sizes for each parameter factor are:

```
long: 4 bytes, int: 2 bytes, short: 1 byte.
```

If a data requires multiple blocks, the byte size will be N times as big as this. (MAXE is a constant 8 in this case.) For each byte, a higher byte is positioned at the high address.

#### List A: Basic Data Structure Define

```c
/* -------------- basic data structure define -------------- */
struct voicedata {
    long          wavst;             /* wave start address           00 */
    long          waved;             /*       end                    04 */
    long          genst;             /* generator start address      08 */
    long          gened;             /*             end              0C */

    int           loop;              /* ga mode status (see gaa)     10 */
    short         loop_sus;          /* loop sustain number (0-8)    12 */
    short         loop_end;          /* loop end    number  (0-8)    13 */
    long          loopst[MAXE];      /* loop start address           14 */
                                     /* --- b15~b12 for loop fine       */
    long          looped[MAXE];      /* loop end   address           34 */
                                     /* --- b15 for jumploop flg        */
    int           loopxf[MAXE];      /* loop x feed time             54 */
    unsigned int  looptm[MAXE];      /* loop time (' or times)       64 */

    int           dcp;               /* dcp voice pitch with detune  74 */
    short         dcf;               /* frequency offset value       76 */
    short         dcq;               /* filter Q  offset value       77 */

    short         dca_sus;           /* dca envelop sustain point    78 */
    short         dca_end;           /* dca envelop end point        79 */
    short         dca_rate[MAXE];    /* dca envelop rate value       7A */
    unsigned short dca_stop[MAXE];   /* dca envelop stop value       82 */

    short         dcf_sus;           /* dcf envelop sustain point    8A */
    short         dcf_end;           /* dcf envelop end point        8B */
    short         dcf_rate[MAXE];    /* dcf envelop rate value       8C */
    unsigned short dcf_stop[MAXE];   /* dcf envelop stop value       94 */

    unsigned int   lfo_delay;        /* lfo delay time               9C */
    unsigned short lfo_name;         /* lfo wave form define b/d sync 0/1  9E */
    unsigned short lfo_atck;         /* lfo attack value             9F */
    short         lfo_rate;          /* lfo rate (-time-increment)   A0 */
    short         lfo_dcp;           /* lfo pitch depth              A1 */
    short         lfo_dca;           /* lfo amp    depth             A2 */
    short         lfo_dcf;           /* lfo filter depth             A3 */
    short         lfo_dcq;           /* lfo filter-Q depth           A4 */
    short         vel_dcq_kf;        /* initial touch dcq follow     A5 */

    short         dca_kf;            /* dca keyboard follow depth    A6 */
    short         dca_rs;            /* dca noterate scaling depth   A7 */
    short         dcf_kf;            /* dcf keyboard follow depth    A8 */
    short         dcf_rs;            /* dcf noterate scaling depth   A9 */

    short         vel_dca_kf;        /* initial touchamp key follow  AA */
    short         vel_dca_rs;        /* initial touchamp rate scale  AB */
    short         vel_dcf_kf;        /* initial touchdcf key follow  AC */
    short         vel_dcf_rs;        /* initial touchdcf rate scale  AD */

    unsigned short hwid;             /* high width MIDI code         AE */
    unsigned short lwid;             /* low                          AF */
    unsigned short cent;             /* keynote center               B0 */

    unsigned short samp;             /* sampling frequency           B1 */

    char           name[14];         /* wave name                    B2 */
};                                   /* total byte ---  0C0             */
```

#### Parameter Descriptions

**wavst, waved** — Shows the head and end addresses of a PCM encoded waveform data for a designated voice by a Word Address. `wavst ≤ waved`.

**genst, gened** — Shows the head and end addresses of the sounding range for a designated voice by a Word Address. `wavst ≤ genst ≤ gened ≤ waved`.

**loop** — Assigns sounding styles for a designated voice.

| Value  | Mode    | Description           |
|--------|---------|-----------------------|
| 0x0000 | NO SOUND| Waveform not yet defined |
| 0x01D7 | NORMAL  | Normal sound          |
| 0x101D | REV     | Reversed sound        |
| 0x2014 | CUE     | Cuing sound           |
| 0x0013 | SYN     | Synthesized waveform  |

**loop_sus** — Assigns the position of Sustain Loop. A number from 0 thru 8 can be taken and 0 denotes Loop 1 execution. The number 0 thru 7 assigns the corresponding loop execution, and 8 denotes no execution of Sustain Loop.

**loop_end** — Assigns the end of multi-loop. A number from 0 thru 8 can be taken and the number 0 denotes the end of loop 1, and the number 0 thru 7 assigns the corresponding loop end. The number 8 assigns execution of all the 8 loops.

**loopst, looped** — Shows the head and end addresses for a looping range by a Word Address. In correspondence with 8 multi-loops, eight sets of the head and the end addresses are provided. Upper 8 bits for `loopst` are used for loop fine and take a number among 0 - 255. The MSB for `looped` is used for loop patterns; 1 for Skip, 0 for Trace. `wavst ≤ genst ≤ loopst ≤ looped ≤ gened ≤ waved`.

**loopxf** — Shows a timing duration for Cross Fade Loop and takes a number among 0 - 1023. The number 0 designates a minimal distorted sound from artificial data in between samples. Place a figure 0 for non-cross fade looping.

**looptm** — Denotes a timing duration for Multi Loop and takes a number among 1 - 1022. The duration can be set by 16 milliseconds from 16 milliseconds up to 16 seconds.

**dcp** — Denotes a pitch. The pitch can be corrected by 1/256 semi-tone by a sound range setting.

**dcf** — Denotes an offset value for Cut Off Frequency on the filter and takes a number among 0 - 127. The frequency will never be lowered than the value set in `dcf` and the value 127 designates the filter should open.

**dcq** — Denotes an offset value for Resonance on the filter and takes a number among 0 - 127; however, notice that the effective bit number is upper 4 bits.

**dca_sus, dcf_sus** — Denote Sustain positions on each envelope on Amp and Filter and take numbers among 0 - 7.

**dca_end, dcf_end** — Denote the end point for an envelope and take a number among 0 - 7.

**dca_rate, dcf_rate** — Denote slopes for an envelope curve. The lower 7 bits will be a number among 0 - 127; an absolute value. The MSB denotes a slope; 0 for plus and 1 for minus.

**dca_stop, dcf_stop** — Denote an arrival value for each step of an envelope curve and take a number 0 - 255.

```
            stop 0
              /\rate1   stop 2                stop 4
        rate1/  \      /\                    /\ rate5  stop 6
            /    \rate2/  \rate3   rate4   /  \  rate5 stop 6
           /      stop1    \________/\____/    \           \
          /                stop 3 sus=3        stop 5  rate6\rate7
         /                                                   \
        /                                                     \
                                                          stop 7=0
                                                          end = 7
```

**lfo_delay** — Denotes a time duration before LFO starts affecting sound and takes a number among 0 - 65535. The LFO delay can be set by 2 milliseconds.

**lfo_name** — Denotes the LFO waveform names:

| Value | Waveform           |
|-------|--------------------|
| 0     | Sine Wave          |
| 1     | Ascending Saw-Tooth|
| 2     | Descending Saw-Tooth|
| 3     | Triangle           |
| 4     | Rectangular        |
| 5     | Random             |

The MSB denotes On or Off for phase synchronization; 0 for Off and 1 for On.

**lfo_atck** — Denotes a rising envelope rate for the LFO effect and takes a number among 1 - 127. A smaller number denotes slower and a bigger number denotes faster.

**lfo_rate** — Denotes a frequency for the LFO and takes a number among 0 - 127.

**lfo_dcp** — Denotes a depth of LFO effect on the pitch and takes a number among 0 - 127.

**lfo_dca** — Denotes a depth of LFO effect on the amplitude and takes a number among 0 - 127.

**lfo_dcf** — Denotes a depth of LFO effect on the filter and takes a number among 0 - 127.

**lfo_dcq** — Denotes a depth of LFO effect on the resonance and takes a number among 0 - 127.

**dca_kf** — Denotes a key follow effect on the amplitude and takes a number among -127 to +127. Centering the original key, "+" assigns upper right and "-" assigns lower right tilt for sound volume.

**dca_rs** — Denotes a rate follow effect for an Amp Envelope and takes a number among -127 to +127. Centering the original key, "+" assigns the sharper rate the higher key and "-" assigns the shallower rate the higher key.

**dcf_kf** — Denotes a key follow parameter on the filter.

**dcf_rs** — Denotes a rate follow effect for a Filter Envelope.

**vel_dca_kf** — Denotes a degree against amplitude made by the initial touch response and takes a number among -127 thru +127. A plus (+) number assigns the higher velocity generates the bigger volume; a minus (-) number assigns the lower velocity generates the bigger sound volume.

**vel_dca_rs** — Denotes an effect rate against an envelope curve made by the initial touch response and takes a number among -127 thru +127. A plus (+) number assigns the higher velocity generates the sharper curve; a minus (-) number assigns the higher velocity generates the gentler slope.

**vel_dcf_kf** — Denotes a filter effect made by the initial touch response.

**vel_dcf_rs** — (Same family as above; rate follow for filter envelope on initial touch.)

**hwid, lwid, cent** — Denote the highest and lowest limitations for a sounding range and the key code for an original sample. Take numbers among 0 thru 127 and have the same note code for keyboard positions as the MIDI standard has.

**samp** — Denotes a sampling frequency when you used for recording a material and takes a number among 0 - 2; 0 for 36kHz, 1 for 18kHz, and 2 for 9kHz.

**name** — Shows a voice name with 12 ASCII-coded characters. A voice name occupies a 14-byte region and the last 2 bytes should be always 0.

### 2-2. Details of Bank

Bank data is an assemblage of the parameters which make up a keyboard setting as an actual instrument with key range settings of key split, velocity split, touch response, and/or MIDI basic channel. The size of the parameters is 656 bytes and the total data of the bank parameters occupies 5,248 bytes maximum as the FZ-1 internal memory can store up to 8 banks. The bank parameters start with the label "bank" in the work area to be addressed by every 656 bytes in the sequence from bank 1 to bank 8.

This content of 656-byte data is shown in the list B. Same as the voice data, it is defined as a structure of "C" and the size for every factor is also the same. MAXV is a constant 64 in the format.

#### List B: Bank Data Structure

```c
/* HP 64000 - C    70116 compiler */
/* ----------------- bank data structure ----------------- */
struct bankdata {
    unsigned int   bstep;            /* bank use voice number          00 */
    unsigned short hwid[MAXV];       /* high keynote width             02 */
    unsigned short lwid[MAXV];       /* low                            42 */
    unsigned short htch[MAXV];       /* high keytouch width            82 */
    unsigned short ltch[MAXV];       /* low                            C2 */
    unsigned short cent[MAXV];       /* keynote center                102 */
    unsigned short mchn[MAXV];       /* generate midi channel         142 */
    unsigned short gchn[MAXV];       /* generate channel select       182 */
    unsigned short bvol[MAXV];       /* generate output level         1C2 */
    unsigned int   vp[MAXV];         /* voice data pointer            202 */
    char           name[14];         /* bank name                     282 */
};                                   /* --- total byte                 290 */
#define BSIZE (sizeof(struct bankdata))
```

#### Parameter Descriptions

**bstep** — Denotes the current number of key splits or the number of voices which the bank uses and takes a number among 0 thru 64. The number 0 denotes that the current bank is not yet defined.

**hwid, lwid** — Denotes the highest and lowest limitations for a key split region. The note code corresponds with that of MIDI. These limitations can be set independently from those for voice data.

**htch, ltch** — Denotes the highest and lowest limitations for a velocity split. The code corresponds with the initial values in the MIDI standard and takes a number among 1 - 127.

**cent** — Denotes an original key position of a key split. The code corresponds with the note code of the MIDI and takes a number among 0 - 127 and the center key can be set independently from voice setting.

**mchn** — Denotes a receiving channel for each area at the Area Mode and takes a number among 0 - 15 corresponding with the MIDI basic channels 1 thru 16.

**gchn** — Denotes a sound generator for a designated area. These 8 bits corresponding with the generator 1 - 8 allow to generate sound when each bit is 1 and prohibit to do so when it is 0. The bit 0 stands for the generator 1. For instance, sound from the area will be generated only by the generators 2 and 7 in the case `gchn=42h`.

**bvol** — Denotes sound volume for each area and takes a number among 0 - 127. This enables to balance sound volumes among the voices which allocated to areas to make up a bank.

**vp** — Is placed with the head address for voice parameters which are used for an area. Notice that the voice number among 0 - 63 is positioned at these bits when the parameters are dumped from the internal memory to disk, or outside memory over MIDI or External Port.

**name** — Shows a bank name with 12-byte ASCII-coded characters. The last 2 bytes should be always 0.

### 2-3. Details of Effect

Effect data is an assemblage of the effect parameters controlled by the pitch bender, the modulation wheel, the foot volume, the after touch, the face panel controls except keyboard keys. The size of the parameters is 24 bytes headed by `pare` in the work area of which content is shown in the list C. The effect data is parameters commonly effective on all banks and all voices. These are defined as a structure of "C" and the size of every factor is all 1 byte.

#### List C: Effect Data Structure

```c
struct effectdata {
    short bend;       /* bender depth                  00 */
    short mvol;       /* master volume value           01 */
    short suss;       /* sustain switch ON,OFF         02 */

    short mod_lfp;    /* modulation  lfo pitch         03 */
    short mod_lfa;    /*             lfo amp           04 */
    short mod_lff;    /*             lfo filter        05 */
    short mod_lfq;    /*             lfo filter-q      06 */
    short mod_dcf;    /*             filter offset     07 */
    short mod_dca;    /*             amp    offset     08 */
    short mod_dcq;    /*             fil q  offset     09 */

    short fot_lfp;    /* foot volume lfo pitch         0A */
    short fot_lfa;    /*             lfo amp           0B */
    short fot_lff;    /*             lfo filter        0C */
    short fot_lfq;    /*             lfo filter-q      0D */
    short fot_dca;    /*             amp    offset     0E */
    short fot_dcf;    /*             filter offset     0F */
    short fot_dcq;    /*             fil q  offset     10 */

    short aft_lfp;    /* after touch lfo pitch         11 */
    short aft_lfa;    /*             lfo amp           12 */
    short aft_lff;    /*             lfo filter        13 */
    short aft_lfq;    /*             lfo filter-q      14 */
    short aft_dca;    /*             amp    offset     15 */
    short aft_dcf;    /*             filter offset     16 */
    short aft_dcq;    /*             fil q  offset     17 */
};                    /* total byte = 18                  */
#define ESIZE (sizeof(struct effectdata))
```

#### Parameter Descriptions

**bend** — Denotes an effect degree of the Pitch Bender by a 1/8 semi-tone step and takes a number among 0 - 127.

**mvol, suss** — Unused. Normally "0" is placed.

**mod / fot / aft × lfp / lfa / lff / lfq / dca / dcf / dcq** — Denotes an influential degree made by each controller and takes a number among 0 - 127. These effects form a matrix:

| Controller \ Target | lfp (Pitch LFO) | lfa (Amp LFO) | lff (Filter LFO) | lfq (Resonance LFO) | dca (Amp Offset) | dcf (Filter Offset) | dcq (Resonance Offset) |
|---------------------|-----------------|---------------|-------------------|---------------------|-------------------|---------------------|------------------------|
| mod (Mod Wheel)     | mod_lfp         | mod_lfa       | mod_lff           | mod_lfq             | mod_dca           | mod_dcf             | mod_dcq                |
| fot (Foot Volume)   | fot_lfp         | fot_lfa       | fot_lff           | fot_lfq             | fot_dca           | fot_dcf             | fot_dcq                |
| aft (After Touch)   | aft_lfp         | aft_lfa       | aft_lff           | aft_lfq             | aft_dca           | aft_dcf             | aft_dcq                |

---

## 3. Outline of "Dump"

The FZ-1 outputs or inputs data over the External Port or the MIDI ports. The data dump features will be explained in detail.

```
Dump Procedures
    ├── External Port
    │       ├── Remote Code
    │       ├── Open Code
    │       ├── Data Code
    │       └── External Port Hardware
    └── MIDI
            ├── MIDI Remote
            ├── MIDI Open/Close
            └── MIDI Data Transfer
```

### 3-1. Procedures for Data Dump

The FZ-1 unit has a capability of inputting and outputting data for waveforms, control parameters, etc. over an external port and MIDI ports as well as a capability of dumping data on micro floppy disks. The data transfer will be done by the following two methods:

#### a) From an FZ-1 unit to another FZ-1 unit

1. Set the Master unit to the Save Mode
2. Set the Slave unit to the Load, Merge or Verify Mode
3. Set the both units to MIDI or to External Port and push the buttons "Enter" and "Yes" on the both units to save/load data onto the disk in the Slave unit.

To transfer 1 Megabyte of data amount to another unit or computer, it will take 20 or 30 minutes over MIDI and 40 seconds over the External Port.

#### b) From an FZ-1 unit to a computer or vice versa

An FZ-1 unit can be hooked up to a computer which is equipped with MIDI compatible ports or with the equivalent External Port to transfer each other waveform data and parameters. The device in the computer should be set to MIDI or the External Port and start up with a command from the computer the connected FZ-1 unit which is set to the Remote Mode. The Remote Mode implies a control of the FZ-1 unit, which is in the Save or Load Mode, from outside.

### 3-1-1. Outline of External Port

#### a) Data transfer from FZ-1 to a computer

```
COMPUTER                          FZ-1
+----------------------+        +----------------------+
| Transmits Remote     | -----> | Recognizes Remote    |
| Code and Save Command|        | Code and Save Commands|
+----------------------+        +----------------------+
| Transition to        |        | Transition to        |
| Recognize Mode       |        | Save Mode            |
+----------------------+        +----------------------+
| Recognizes           | <----- | Transmits            |
| Open Code            |        | Open Code            |
+----------------------+        +----------------------+
| Recognizes           | <----- | Transmits            |
| Data Code            |        | Data Code            |
+----------------------+        +----------------------+
```

#### b) Data Transfer from a Computer to FZ-1

```
COMPUTER                          FZ-1
+----------------------+        +----------------------+
| Transmits Remote     | -----> | Recognizes Remote    |
| Code and Load Command|        | Code and Load Command|
+----------------------+        +----------------------+
| Waits for One Second |        | Transition to        |
|                      |        | Load Mode            |
+----------------------+        +----------------------+
| Transmits            | -----> | Recognizes           |
| Open Code            |        | Open Code            |
+----------------------+        +----------------------+
| Transmits            | -----> | Recognizes           |
| Data Code            |        | Data Code            |
+----------------------+        +----------------------+
```

Note: Merge and Verify are done in the same way as Load.

### 3-1-2. Details of Remote Code

The Remote Code, a 17-byte data block as illustrated below, is effective only for data transfer from a computer to an FZ-1 unit.

```
0                                                              16
[7F][  ][  ][  ][  ][eb][ev][sta][mod][  ][  ][  ][  ][  ][  ][sum]
```

The mark `[ ]` denotes data of a byte and transfer will be done from the left to right. Blank bytes have no meaning and are normally filled with 0.

**[7F]** — Is a constant of hexadecimal 7F and denotes the header for the remote code. When an FZ-1 receives 7F in its waiting status, the unit recognizes the following as remote codes.

**[eb]** — Denotes Edit Bank, which will change the bank number while being edited inside an FZ-1 unit. The values from 0 thru 7 stand for the banks 1 thru 8. The value 7F means no bank number change to stay the number unchanged as it is.

**[ev]** — Denotes Edit Voice, which will change the voice number while being edited inside an FZ-1 unit. The values 0 thru 63 stand for the voices 1 thru 64. The value 7F means no voice number change to stay the number unchanged as it is.

**[sta]** — Designates the data which will be transmitted and the value will be among 0 thru 3.

| sta | Name   | Data To Be Designated                            |
|-----|--------|--------------------------------------------------|
| 0   | FULL   | Entire data saved in the FZ-1 internal memory   |
| 1   | VOICE  | Data for voices, waveforms designated by "ev"   |
| 2   | BANK   | Data for banks, voices, waveforms designated by "eb" |
| 3   | EFFECT | Data for effects saved in the FZ-1 internal memory |

**[mod]** — Designates a destination and a processing for the data which will be transmitted and the value will be among 0 thru 3.

| mod | Name   | Destination          | Processing                                            |
|-----|--------|----------------------|-------------------------------------------------------|
| 0   | SAVE   | from FZ-1 to Computer| Transmit internal data                                |
| 1   | LOAD   | from Computer to FZ-1| Load received data to internal memory                 |
| 2   | MERGE  | from Computer to FZ-1| Merge received data with data in internal memory      |
| 3   | VERIFY | from Computer to FZ-1| Compare and check received data with data existing in internal memory |

**[sum]** — Denotes check sum. A value is placed to be complement for 2 after adding all data figures of 16 bytes (0 thru 15).

The FZ-1 in the Remote Mode changes its mode according to the designated codes, when the unit receives the afore-mentioned codes. The same mode changes occur for a linkage between 2 units of FZ-1 after the buttons "Enter" and "Yes" are depressed on the slave unit end.

### 3-1-3. Details of Open Code

The Open Code is a 17-byte data block determining a size of the following data code before execution of the data transfer. The 17-byte data block is illustrated as follows:

```
0                                                                       16
[sta][IL][IH][bn][Vn][WL][WH][eb][ev][  ][  ][  ][  ][  ][  ][  ][sum]
```

**[sta]** — Is a header designating the data which will follow to be transmitted like the [sta] byte for the Remote Code.

**[IL][IH]** — Is a 2-byte value for determining a block size of the data code.

**[bn]** — Determines the number of banks of which data will be included in the followingly transmitted data block. The value will be among 0 thru 8 and "0" denotes no bank data in the coming data block.

**[Vn]** — Determines the number of voices of which data will be included in the followingly transmitted data block. The value will be among 0 thru 64 and "0" denotes no voice data in the coming data block.

**[WL][WH]** — Determines the number of PCM-sampled waveforms of which data will be included in the succeedingly transmitted data block. A data unit consists of 1024 bytes and 512 samples and fragments will be rounded up. "0" denotes no PCM-sampled waveform data in the coming block.

**[eb][ev][sum]** — Denote the same as those of the Remote Code do.

The relations among data/parameters will be as follows:

| Name   | Data Contents Included                                    | sta | bn | vn | WHL |
|--------|-----------------------------------------------------------|-----|----|----|-----|
| FULL   | Entire data                                               | 0   | N  | N  | N   |
| BANK   | Bank parameters and voice parameters/PCM waveform data within the bank | 2   | 1  | N  | N   |
| VOICE  | Voice parameters and PCM                                  | 1   | 0  | 1  | N   |
| EFFECT | Effect parameters                                         | 3   | 0  | 0  | 0   |
| PARA   | Entire parameters                                         | 0   | N  | N  | 0   |

"N" in the above table denotes a natural figure more than 0.

The transfer of only "PARA" is rarely executed and there is no possibility of the execution in the save command for data communication from an FZ-1 to a computer or to another FZ-1 unit. The "PARA" revises only existing parameters and transmits no waveform data. The "PARA" works effectively for the waveform data saved last in the internal memory. Address management should be done at the transmitting side, i.e., the management should be done at the connected computer which transmits the "PARA" data. The other Name-Data-Value combinations than the above table are prohibited to use; therefore, it cannot be done to revise nothing but the parameters for a particular bank or particular voice.

### 3-1-4. Details of Data Code

Data Code is a data entity being transmitted by every 1025 bytes (consisting of a 1024-byte data and a 1-byte check sum) as a data unit.

```
+----+----+----+----+
| d0L| d0H| d1L| d1H| ...
+----+----+----+----+
|                   |
|  a 1024-byte data |
|                   |
+----+              |
| sum|              |
+----+--------------+
```

In regard to the details of data, refer to the Outline of Parameters.
In regard to the way of packing into a 1024-byte data, refer to the Outline of Disk Format.

### 3-1-5. Details of the External Port — the hardware

An FZ-1 machine is equipped with an External (Input/Output) Port and the machine sends and receives over the External Port data each for Full, Bank, Voice, Effect, Optional Application Program, Sequences, etc., utilizing its 8-bit bidirectional data port in the mode 2 and its another 8-bit data input port in the mode 0 of a Programmable Parallel Interface Unit (PIU) consisting of a uPD71055 chip. Refer to the schematic diagram for the FZ-1 External I/O and the documentation of the NEC's MOS IC uPD71055.

#### a) Device

The uPD71055, a CMOS chip of NEC make, is completely compatible with an Intel's 8255 nMOS-type parallel interface chip. The uPD71055 has a set of 3 programmable 8-bit Input/Output ports (Port 0, Port 1 and Port 2) of which functions are described below:

- **Port 0**: Is equipped with an independent 8-bit register for input and operates not only as an 8-bit unidirectional I/O port but also as an 8-bit bidirectional data port in its mode 2.
- **Port 1**: Is equipped with an 8-bit register which operates either for input or output and works as an 8-bit I/O port.
- **Port 2**: Is equipped with two registers which works as independent I/O ports against the divided upper 4-bit and lower 4-bit data. In the modes 1 and 2, this port functions for interrupts as well as peripheral control signals. For the purpose the Port 2 allows an operation for Set or Reset by a bit.

#### b) Operation

For transmitting data, the FZ-1 first confirms a sending-standby status `^O^B^F=High` on its end and also a receiving-standby status `^B^U^S^Y=High` on the end of a connected machine. Following the confirmation, the output data is sent to the uPD71055 to output only the signal `^S^T^B^O` and does not transfer the very data to a bus.

The uPD71055 latches data into its buffer at the rising edge of a signal `^W^R` and simultaneously lowers the signal `^O^B^F` level. The signal `^O^B^F` raises its level at a timing of the signal `^A^C^K` drop. The FZ-1 machine which is ready to be input detects polling the INT signal fed from the uPD71055 and lowers the level of signals `^B^U^S^Y^O` and `^A^C^K^O` when confirming the INT signal leveled at High. By lowering the signal `^A^C^K^O` level to Low, the machine raises the signal `^O^B^F` on the output side at High in order to send out data to a bus.

After the data which outcoming to the bus is latched and held in, the machine finishes the transfer of one-byte data returning the signals `^B^U^S^Y^O` and `^A^C^K^O` to High level. See the Charts 1 and 2 for the examples of programming.

#### Chart 1: External Port Connection

```
            STBI
            I/O Address
+---------+                            +-----------+
|  FZ-1   |        15                  |           |
|         |   ID <-------> Odd Number  |    ID     |
| uPD71055|                            |           |
|         |   IBF  |12                 |   IBF     |
|         |   STB  |13   AND 19        |   STB     |
|         |   OBF  |10                 |   OBF     |
|         |   STBO |15   22            |   STBO    |
|         |   ACK  |11   21            |   ACK     |
|         |   ACKO |14    8            |   ACKO    |
|  IF IN  |   BUSY |24   24            |   BUSY    |
| IF OUT  |   BUSYO|16   25            |   BUSYO   |
+---------+                            +-----------+
              uPD71055              D-Sub 25-pin connector
```

#### Chart 2: External Port Read/Write Timing

```
              0.25 usec
WR    ─────────┐  ┌─────────────────────
              1.5 usec
STBO  ──────────┐ ┌────────────────────
                └─┘
OBF   ──┐                        ┌─────
        └────────────────────────┘
ACK   ──────────────┐    ┌────────────
                    └────┘
[DATA]──────────⟨    Data    ⟩─────────
                0.25 usec
STB   ──┐ ┌──────────────────────────
        └─┘
INT   ──┐                        ┌────
        └────────────────────────┘
ACKO  ─────────────┐        ┌─────────
                   └────────┘
BUSY  ─────────────┐        ┌─────────
(IF OUT)           └────────┘
RD    ─────────────────┐ ┌────────────
                       └─┘
                       0.25 usec
IBF   ────────┐                ┌──────
              └────────────────┘
```

A: Master (Save) 1 Byte Transport
B: Slave (Load) 1 Byte Receive

#### c) Examples of Input and Output Programs

c-1) Output Program:

```asm
portout  IN     AL, PIA12          ; ^O^B^F==H ?    Confirms a standby
         AND    AL, #+00080H       ;                status for output
         BZ     SHORT Rportout     ;
         IN     AL, PIA11          ; ^B^U^S^Y==H ?
         AND    AL, #+00040H       ;
         BZ     SHORT Rportout

         MOV    AL, CL             ; CL: Output Data
         OUT    IO1S, AL
         MOV    AL, #+0005H        ; ^S^T^B^O=L
         OUT    PIA12, AL
         MOV    AL, #+00007        ; ^S^T^B^O=H
         OUT    PIA12, AL
Rportout RET
```

c-2) Input Program:

```asm
portin   IN     AL, PIA12          ; INT==H ?
         AND    AL, #+00008H       ;
         BZ     SHORT Rportin

         MOV    AL, #+00002H       ; ^S^T^B^O & ^A^C^K^O = L
         OUT    PIA12, AL

         OUT    STBI, AL           ; Latches data (AL:dummy)

         IN     AL, IO1S           ; Inputs data to AL

         MOV    BL, AL             ; Stores data in BL

         MOV    AL, #+00007        ; ^S^T^B^O & ^A^C^K^O = H
         OUT    PIA12, AL
Rportin  RET
```

### 3-2. Outline of MIDI

The data transfer over MIDI is executed as follows.

#### a) Data transfer from an FZ-1 to a computer

```
COMPUTER                          FZ-1
+----------------------+        +----------------------+
| Transmits            | -----> | Recognizes           |
| MIDI Remote Save Cmd |        | MIDI Remote Save Cmd |
+----------------------+        +----------------------+
| Waits for Coming     |        | Transits to          |
| MIDI Inputs          |        | Save Mode            |
+----------------------+        +----------------------+
| Recognizes Open MIDI | <----- | Transmits Open MIDI  |
+----------------------+        +----------------------+
| Transmits OK MIDI    | -----> | Recognizes OK MIDI   |
+----------------------+        +----------------------+
| Recognizes Data MIDI | <----- | Transmits Data MIDI  |
+----------------------+        +----------------------+      Repeat
| Transmits OK MIDI    | -----> | Recognizes OK MIDI   |
+----------------------+        +----------------------+
| Recognizes Close MIDI| <----- | Transmits Close MIDI |
+----------------------+        +----------------------+
```

#### b) Data transfer from a computer to an FZ-1

```
COMPUTER                          FZ-1
+----------------------+        +----------------------+
| Transmits Remote     | -----> | Recognizes Remote    |
| Load Command         |        | Load Command         |
+----------------------+        +----------------------+
| Waits for one second |        | Transits to Load Mode|
+----------------------+        +----------------------+
| Transmits Open MIDI  | -----> | Recognizes Open MIDI |
+----------------------+        +----------------------+
| Recognizes OK MIDI   | <----- | Transmits OK MIDI    |
+----------------------+        +----------------------+
| Transmits Data MIDI  | -----> | Recognizes Data MIDI |
+----------------------+        +----------------------+      Repeat
| Recognizes OK MIDI   | <----- | Transmits OK MIDI    |
+----------------------+        +----------------------+
| Transmits Close MIDI | -----> | Recognizes Close MIDI|
+----------------------+        +----------------------+
```

### 3-2-1. Details of Remote MIDI

The Remote MIDI is a MIDI System Exclusive Code which is exclusively provided for data transfer from a computer to an FZ-1 unit.

```
[F0][44][02][00][7n]......[7F].....[eb][ev][sta][mod]....[F7]
```

In this appendix the mark `[ ]` denotes a byte data and the transfer will be executed from the left to the right.

**[7n]** — For this byte, "n" for a Basic Channel number is to be placed in the lower 4-bit portion, and 7 is to be placed in the upper 4-bit portion. This is used for selective remote control in the case of plural data connections.

**[eb], [ev], [sta], [mod]** — Same as in the details of Remote Code.

### 3-2-2. Details of Open/Close MIDI

#### A) Details of Open MIDI

The Open MIDI is a MIDI Exclusive Code determining a size of the data code which will be transferred preceding MIDI data.

```
[F0][44][02][00][7n][70]......[sta]......[bn][Vn][0W0][0W1][0W2][0W3][eb][ev]....[F7]
```

| Field    | Meaning                                                            |
|----------|--------------------------------------------------------------------|
| [sta]    | Status — Same as the details of Open Code                          |
| [bn]     | Bank Number — Same as the details of Open Code                     |
| [vn]     | Voice Number — Same as the details of Open Code                    |
| [0W0]    | Wave Number — Determines the number of PCM-sampled waveforms within the transmitted data. A data unit consists of 1024 bytes (512 samples). The original value is of 2 bytes and developed by 4 bits into 4 bytes to output as a MIDI code. "W3" should be a higher bit. |
| [eb]     | Edit Bank — Same as the details of Remote Code                     |
| [ev]     | Edit Voice — Same as the details of Remote Code                    |

#### B) Details of OK MIDI

The OK MIDI is a MIDI Exclusive Data to be sent to the data transmitting end as an answer message to the code Open MIDI or Data MIDI.

```
[F0][44][02][00][7n]......[72].........[F7]
```

If a format or a check sum is wrong for the latest data which have been received, the message ERR MIDI is transmitted instead of OK MIDI. The code is as follows:

```
[F0][44][02][00][7n]......[73]..........[F7]
```

Receiving the message ERR MIDI the data transmitting end will respond the following:

- a) Against the Open MIDI code, the failure in Open will show the Data Error on its screen.
- b) Against the Data MIDI, the machine will transmit again last data.

#### C) Details of Data MIDI

The data code is an entity of the data to be transmitted. The data will be developed into 2 bytes from a byte for transmit. The 64 bytes of data will be developed into 128 bytes for one-time transmit.

```
[F0][44][02][00][7n]......[74]......[0dL][0dH][0dL][ ........ ].....[Msum]....[F7]
                                           <----- 128 bytes ----->
```

**[0dL][0dH]** — "0" is input in their upper 4-bit portions after one byte of data is developed into lower 4 bits and into upper 4 bits.

**[Msum]** — Denotes a check sum. The value comes from the logical AND function on "7F" and "a complement for 2" of the total addition number for developed 128 bytes.

Same as Disk or Port, 1024 bytes of data are regarded as a data unit. For the transfer of this, a transaction between the Data MIDI and the OK MIDI repeats itself 16 times. Refer to the outline of Parameters for the details of Data and also refer to the outline of Disk for the way of packing data into 1024 bytes.

#### D) Details of Effect MIDI

```
[F0][44][02][00][7n]..........[78]................[en][vv]......[F7]
```

"en" denotes an effect number:
The number 00 for the bender depth, and the numbers 01 and 02 are unused.
"vv" denotes a value among the figures 00 thru 7F.

| en       | lfo pitch | lfo amp | lfa filter | dca | dcf |
|----------|-----------|---------|------------|-----|-----|
| mod w    | 03        | 04      | 05         | 07  | 08  |
| foot v   | 0A        | 0B      | 0C         | 0E  | 0F  |
| after t  | 11        | 12      | 13         | 15  | 16  |

Note: Exclusive info for Effect is transmitted the same way as the Control Commands.

#### E) Details of Close MIDI

The Close MIDI is a MIDI System Exclusive Data which will be transmitted to the data receiver succeeding to the end of Data MIDI transfer.

```
[F0][44][02][00][7n]............[71]..............[F7]
```

---

## 4. Optional Software

The FZ-1 is capable of loading expanded software and executing the program. This chapter offers specific knowledge on the FZ-1 to developers of optional software.

```
Expanded Program
    ├── ROM Entry
    ├── Work Address
    └── Program Example
```

### 4-1. Expanded Program

The FZ-1 features that the data in a program file (ext=5) on its disk can be loaded to an address 6000h and after in the memory of the CPU work area and the FAR CALL to the address 6000h can be executed.

The FZ-1 installs a V50 chip (of NEC make) for the CPU. Since the V50 is upper compatible at the code level with Intel's 8086, you can develop programs on the chip 8086, a much more popular microprocessor.

The tools necessary for development of programs will be:

a) Assembler and compiler for V50 (or 8086)
b) Conversion tool from Object File to FZ-1 Program File

For the details of FZ-1 Program File, refer to the Outline of "Disk". Expanded programs should be created at 6000h for its execution address and the 36k byte area (6000h - EFFFh) can be used.

### 4-2. ROM Entry

The FZ-1 is well designed so that every sub-routine existing in the firmware can be utilized with Break. The sub-routines are named by function numbers. There exists two types of parameters for each sub-routine; some are given in stack and the others are given directly to the WORK AREA. A sub-routine which returns a value has always a value in BW (BX for the 8086). Registers which are retained at all the sub-routines are nothing but SP and BP. The segments except DS1 are retained.

An example of a sub-routine call is shown below:

```
Function No. 51
mpx (d1, d2, d3);
       │
       ▼
push  DS0:WORDPTR d3
push  DS0:WORDPTR d2
push  DS0:WORDPTR d1
push  #51
BRK   3
ADD   SP, #8
```

Succeeding to the push to stack behind the parameters and the final push of the function number, the command `BRK 3` execution makes it possible to call "mpx" the sub-routine within the ROM.

### 4-3. Work Address

The work addresses to be used for the FZ-1 will be shown in the list E. For details of each work, refer to the details of FZ-1 Work.

#### List E: Work Address Table

```
Address  Name        Type  Size   Description
-------  ----------  ----  -----  --------------------------------
0400     keycount    DBS   1      numbers of key_on
0401     lastresp    DBS   1      the last touch response value 1-127
0402     keymap      DBS   8      key on/off table
040A     sch         DBS   2      big timer counter
040C     olddca      DBS   16     generater data
041C     newdca      DBS   16
042C     key         DBS   1      console key code
042D     kkk         DBS   1
042E     kls         DBS   1
042F     sls         DBS   1
0430     ki0         DBS   4      repeat counter for console key
0434     ki1         DBS   4
0439     rpc         DBS   2      adc1 static
043A     adc1        DBS   8
0442     adcb1       DBS   2      ad convert value of line or mic in
0444     env         DBS   1      ad convert value of entry volume
0445     vol         DBS   1      last ad convert value
0446     old         DBS   8      max ad value
044E     max         DBS   8      min ad value
0456     min         DBS   8
045E     cenh        DBS   1      center high limit for bender
045F     cenl        DBS   1      center low limit for bender
0460     stat        DBS   3      midi status byte
0463     parl        DBS   3      midi first data byte
0466     nownote     DBS   2      last MIDI key code and response
0468     genbit      DBS   2      generater bit-num
046A     lastiy      DBS   2      last generater pointer
046C     excn        DBS   1      exclusive-midi data counter
046D     nowled      DBS   1      now led
046E     rand        DBS   2      random number for lfo generate
0470     jump0       DBS   2      con()'s static
0472     lev         DBS   1
0473     lv0         DBS   3
0476     dm          DBS   1
0477     dm0         DBS   3
047A     sm          DBS   1
047B     sm0         DBS   3
047E     mm          DBS   1
047F     mm0         DBS   3
0482     lpos        DBS   2      para_change() parameter
0484     cpos        DBS   2
0486     lmax        DBS   2
0488     cmax        DBS   2
048A     loff        DBS   2
048C     ltop        DBS   2
048E     vpos        DBS   2
0490     posv        DBS   2      graph() parameter
0492     posp        DBS   2
0494     vhi         DBS   2
0496     wid         DBS   4
049A     pos         DBS   16
04AA     grast       DBS   4
04AE     graed       DBS   4
04B2     pp1st       DBS   4
04B6     pp1ed       DBS   4
04BA     pp2st       DBS   4
04BE     pp2ed       DBS   4
04C2     mcount      DBS   2
04C4     mlevel      DBS   2      meter() or jobbing() static
04C6     mpeek       DBS   2
04C8     mtrig       DBS   2
04CA     bb0         DBS   1
04CB     bb1         DBS   1      brink() static
04CC     l_pos       DBS   4
04D0     l_cur       DBS   2
04D2     l_vhi       DBS   2
04D4     l_brk       DBS   2      d_graph() static
04D6     trig        DBS   1      recording trig level (0-255)
04D7     rmod        DBS   1      recording mode
04D8     gain        DBS   1      recording gain 0=L 1=H
04D9     rate        DBS   1      sampling rate (0:36kHz, 1:18kHz, 2:9kHz)
04DA     length      DBS   2      recording length (10msec)
04DC     sintable    DBS   48     sin table for sin synthesis
050C     add_v1      DBS   1      source 1 voice # (0-63)
050D     add_v2      DBS   1      source 2 voice # (0-63)
050E     add_l1      DBS   1      source 1 mix level (0-255)
050F     add_l2      DBS   1      source 2 mix level (0-255)
0510     add_t1      DBS   1      source 1 detune (-127 to 127)
0511     add_t2      DBS   1      source 2 detune (-127 to 127)
0512     add_dly     DBS   4      source 2 delay WORD address
0516     add_xs1     DBS   4      xmix start WORD address
051A     add_xs2     DBS   4      xmix end   WORD address
051E     devnum      DBS   2      device number (0:FDD, 1:MIDI, 2:PORT)
0520     restat      DBS   2      remote() static
0522     remode      DBS   2
0524     cat         DBS   160    cluster allocation table
05C4     dloc        DBS   2      disk location counter
05C6     xysheet     DBS   768    lcd graphic dot image
08C6     voice_num   DBS   2      disk subroutine's static
08C8     bank_num    DBS   2
08CA     wave_num    DBS   2
08CC     cnv_sta     DBS   2
08CE     cnv_pos     DBS   2
08D0     cnv_rp      DBS   4
08D4     memsize     DBS   2      wave memory size (*64Kbyte)
08D6     mi          DBS   260    midi input ring buffer
09DA     mo          DBS   260    midi output ring buffer
0ADE     kb          DBS   260    keyboard input ring buffer
0BE2     si          DBS   260    sequencer input ring buffer
0CE6     so          DBS   260    sequencer output ring buffer
0DEA     midirev     DBS   1      midi recieve channel
0DEB     midisnd     DBS   1      midi send channel
0DEC     midimsk     DBS   1      midi mask status
0DED     midiprg     DBS   1      midi program change register (-1:MASK)
0DEE     seq         DBS   2      sequencer status
0DF0     godtime     DBS   4      god time for sequencer
0DF4     oldtime     DBS   4      old time for sequencer
0DF8     tempo       DBS   2
0DFA     mastertune  DBS   2
0DFC     pbn         DBS   2      play bank number (0-7)
0DFE     pb          DBS   4      bankp[pbn]
0E02     evn         DBS   2      edit voice number (0-63)
0E04     ev          DBS   4      &voic[evn]
0E08     bank        DBS   5248   struct bankdata bank[8]
2288     voic        DBS   12288  struct voicedata voic[64]
5288     pare        DBS   24     struct paradata para
52A0     nowe        DBS   384    run time paradata nowe[16]
5420     gene        DBS   464    run time generater data
55F0     psa         DBS   2      rom entry static
55F2     pca         DBS   2
55F4     pwa         DBS   2
```

### ROM Entry Function Reference

(Selected entries from the function listing in the source document. Format: name / func / usage.)

```
FUNCTION No. 0    entry        : all system initialize       entry();
FUNCTION No. 6    mgetc        : get char now                c = mgetc();  c = ERROR(-1) if no key
FUNCTION No. 7    unmgetc      : back/get char now           unmgetc(c);   c = return key code

Console switch defines:
  0-9    tenkey 0-9
  10     increment value
  11     decrement value
  16     move up cursor
  17     down
  18     right
  19     left
  20     entry menu
  21     escape
  22     display mode change
  23     play mode
  24     parameter modify
  25     set/menu switch
  26     transpose
  27     tunning

FUNCTION No. 8    contsw       : check continue push switch  push = contsw(c); OK:continue push, ERROR=-1
FUNCTION No. 10   mvol         : read main volume            v = mvol();   v = 0-127, ERROR=-1
FUNCTION No. 11   evol         : read envelop value          v = evol();
FUNCTION No. 20   all_noteoff  : all note off                all_noteoff();
FUNCTION No. 21   all_midi_chn : all midi chn                all_midichn(chan); midi off by select dev
FUNCTION No. 22   control_on   : send now control value to MIDI out    control_on();
FUNCTION No. 23   control_off  : control initialize to MIDI out         control_off();
FUNCTION No. 26   noteget      : note key code; read entry volume      v = noteget();
                                  if v = TOUCH:KEY CODE   hbyte=lbyte
                                  else v = MIDI note      KEY CODE is MIDI touch
FUNCTION No. 42   gene_off     : generater sound off         gene_off();
                                                              gene_off(g); int g: generator number (0-7)
FUNCTION No. 43   gabinit      : gate array initialize do    gabinit();
FUNCTION No. 45   rec_start    : send record start command to gaa       rec_start();
                                  rec_start(vn,pre); struct voicedata *vn; record voice
                                                     unsigned int pre;  pre record length
FUNCTION No. 46   rec_trig     : start post recording by line-1         rec_trig();
FUNCTION No. 47   rec_stop     : recording stop              rec_stop(vn,pre);
FUNCTION No. 48   set_gain     : set recording gain          set_gain(g);
FUNCTION No. 49   now_status   : read now generater status byte         now_st();
FUNCTION No. 51   mpx          : multiplex                   mpx(d1,d2,d3);  if d3=0xFF then no_send_MIDI
                                                                              if d2's MSB=1 then send_MIDI
                                                                              if d3=0xFF then 2 byte code
FUNCTION No. 52   key_in       : read data from ring buffer  data = keyin(kn);
                                                              int kn; key_number 0=KEY, 1=MIDI, 2=SEQ
                                                              ERROR then no data
FUNCTION No. 63   chk_func     : check function # from lv0[0],lv0[1],lv0[2]
                                                              fn = chk_func();
FUNCTION No. 65   set_func     : set function # from lv0[0],lv0[1],lv0[2]
                                                              fn = set_func();
FUNCTION No. 66   meter        : simulate audio level meter  meter(v);  ERROR=open meter, else value
FUNCTION No. 69   tenkey       : tenkey data input subroutine v = tenkey(v,para);
FUNCTION No. 70   bitkey       : bitport set 000*000*         bitkey(l,v);
FUNCTION No. 71   ascii        : ascii char data input subroutine  ascii(c,l,s,n); colmn,line,mode-in print
FUNCTION No. 73   d_change     : display change parameter with value, key or switch
                                                              d_change(mode,v,para);
FUNCTION No. 74   d_change_all : display change with offset d1   d_change_all(mode,ppp);  OK=new, ERROR=only value
FUNCTION No. 75   brink        : brink timer counter         b = brink();
FUNCTION No. 76   graph        : graphic edit wave point      c = graphi(mode,ppp1,ppp2);
                                                              0 = only display, nocursor; ERROR-1 for end
                                                              1 = ppp1 set
                                                              2 = ppp1 & ppp2
                                                              cursor pos, v1 offset (WORD)
                                                              cursor pos, v2 offset (WORD)
FUNCTION No. 77   d_graph      : draw for graph subroutine    c = d_graph(mode,pos,wid,cur,vhi);
FUNCTION No. 79   egraph       : envelop data graphic subroutine  c = egraph(p,e);  struct envelop *e;
FUNCTION No. 81   printvn      : display change with offset d1  printvn();
FUNCTION No. 82   printst      : print voice number with status   printst();
FUNCTION No. 83   printbn      : print bank number            printbn();
FUNCTION No. 84   print_name   : print name char (****)     print_name(l,m,c,name);
                                                              print_name(l,m,c,name); line, mode
                                                              char*nl; source-name-string
FUNCTION No. 85   print_num    : print out number            print_num(c,l,m,o,v);  o: position width+1
FUNCTION No. 86   yes_no       : operation yes no question   yes_no(_,l,s_);  some message ( 15 )
FUNCTION No. 87   jobbing      : animation display in jobbing  jobbing();
FUNCTION No. 88   end_job      : message end of job and wait new keyin
                                                              end_job(); int e; error_flag (fdd or copy)
                                                                          int l;line position
                                                                          define_in_yes_no();
FUNCTION No. 96   envelop      : envelop operation           envelop(vv); int vv; (0:DCA, 1:DCF)
FUNCTION No. 97   loop_set     : loop set operation          loop_set();
FUNCTION No. 98   lfo_set      : lfo set operation           lfo_set();
FUNCTION No. 99   tuning       : tuning                      tuning();
FUNCTION No. 100  define_voice : key set operation           define_voice();
FUNCTION No. 101  create_bank  : define bank                 create_bank();
FUNCTION No. 102  delete_voice : delete voice operation      delete_voice();
FUNCTION No. 103  define_bank  : define_bank operation       define_bank();
FUNCTION No. 104  delete_bank  : delete bank                 delete_bank();
FUNCTION No. 105  bender_range : bender range operation      bender_range();
FUNCTION No. 106  vol_dev      : volume device editor (modul,after,foot)
FUNCTION No. 107  midi_function: midi function operation     midi_function();
FUNCTION No. 108  set_copy     : set copy voice or bank number  set_copy(mode); 
                                                              mode = VOICE or BANKE
                                                              VOICECPY 0, VOICEREP 1, BANKCPY 2, BANKREP 3
FUNCTION No. 109  send_excl    : send exclusive effect midi  send_excl(en,ev); int en,ev: effect number, value
FUNCTION No. 110  define_voice : define voice                define_voice();
FUNCTION No. 111  keyboard_set : keyboard_set                keyboard_set();
FUNCTION No. 112  level_fix    : level_fix                   level_fix();
FUNCTION No. 113  length_set   : set length and sampling freq   length_set();
FUNCTION No. 114  length_limit_select  length_limit();
FUNCTION No. 115  rec_do       : recording operation         rec_do(mode); (OK for auto, ERROR for manual)
                                                                            int mode; trig's msb use for fast rectrig,
                                                                            1 mean manual trig, see in ADCA.S
                                                                            fast envelop mode.
FUNCTION No. 118  init_voice   : initialize voice data after recording
                                                              init_voice(vp,ll); struct voicedata *vp; voicedata pointer
                                                              long ll;          voice length
                                                              if ll is zero, set zero. if vp->loop is null
                                                              then all parameter initialize, address only.
FUNCTION No. 119  rec_delete   : rec delete                  err = check_delete(vv);  voice pointer return
FUNCTION No. 120  rpc_wdelete  : delete wave without key pos.name
FUNCTION No. 121  preset_wave  : set preset wave data        preset_wave();
FUNCTION No. 123  sin_add      : set sin add synthesizer parameters
                                                              sin_add(); unsigned short sintable[MAXSIN]; sin add table=96
FUNCTION No. 124  cut_sample   : cut from pcm data           cut_sample(); 1=for rev select; 2=for mix,x_mix select
FUNCTION No. 126  add_select   : add voice number 1,2 (0-63)  short add_v1,add_v2;
                                                              add_select(mode); int mode
FUNCTION No. 127  add_level    : add level                   add_level();
FUNCTION No. 129  add_delay    : add delay address offset    add_delay(); long add_dly;
FUNCTION No. 130  add_detune   : detune tune (-127 to 127)   add_detune(); short add_t1,add_t2;
FUNCTION No. 131  add_cross    : start_cross_address (WORD address) / end cross address (WORD address)
                                                              add_cross(); long add_xs1; long add_xs2;
FUNCTION No. 132  mix_exe      : mix excute (0=MIX, 1=CROSS, 2=REVERSE)  mix_exe(); int mode;
FUNCTION No. 133  init_synthe  : init synthesizer            initialize for one wave synthesizer
                                                              init_synthe(); int st; OK first set, ERROR second set
FUNCTION No. 134  preset_write : preset_write calculator for PCM_data
                                                              preset_write(p); int p: preset wave number
                                                              0=saw-Tooth, 1=Square, 2=Pulse, 3=Sin, 4=Double-sin,
                                                              5=Saw-pulse, 6=random, else nop
FUNCTION No. 135  sin_write    : sin write
FUNCTION No. 136  cut_write    : cut write calculator for PCM data
                                                              cut_write(); long st,ed; cut_data_address (WORD)
FUNCTION No. 137  mix          : mix calculator for PCM data int mix_mode; OK_for_mix, ERROR for x_mix
FUNCTION No. 138  rev          : rev calculator for PCM data rev();
FUNCTION No. 139  load_all     : data transfer waveform DISK,PORT,MIDI
                                                              load_all(mode,dev,sta,name);
                                                              int mode; load mode (LOAD,MERGE,VERIFY)
                                                              int dev;  device number (DISK,PORT,MIDI)
                                                              int sta;  DATA,STAT,BANK,VOICE
                                                              char*name; file name (in-use dev=DISK)
FUNCTION No. 140  del_asbefor  : data transfer from device to wavemem
                                                              del_asbefor(mode,dev,sta,name);
FUNCTION No. 141  save_all     : data transmit waveform to DISK,PORT,MIDI
                                                              save_all(mode,sta,name);
FUNCTION No. 142  erase_all    : erase disk file             erase_all();
FUNCTION No. 143  format_all   : format disk                 format_all();
FUNCTION No. 144  print_dev    : listing voice bank files in DISK
FUNCTION No. 145  print_dev_name : print device name         int sta; data status
FUNCTION No. 147  mopen        : open file                   err = mopen(name,ext,fbuf,dbuf);
                                                              ERROR=open error
                                                              char*name; file name
                                                              int ext;   file status
                                                              struct fcb *fbuf;   -> 260 byte work area
                                                              char dbuf[SYSSIZ];  -> 1024 byte buffer
FUNCTION No. 148  mcreat       : create file                 err = mcreat(name,ext,fbuf,dbuf); ERROR=mcreat miss
FUNCTION No. 149  mclose       : mclose open file            err = mclose(fbuf,dbuf); int err; mclose error
FUNCTION No. 150  mread        : mread 1s cluster (1s * 1024 byte)
                                                              err = mread(f,1s,data); int 1s; mread cluster #
FUNCTION No. 151  mwrite       : mwrite 1s cluster (1s * 256 byte)
                                                              err = mwrite(f,1s,data); int err; mwrite error
                                                              if datasize less than 1s*1024, system data buffer
FUNCTION No. 152  delete       : delete file                 err = delete(name,ext); int err; delete error
                                                                                       int ext; file status
FUNCTION No. 153  serch        : serch file name in dir      catserch();
                                                              char*name; file name
                                                              int ext;   file extension status
                                                              int err;   0xFF; terminated string
                                                              struct.sysparr*d;
                                                              remark: Use this function call before disk operation; fdc.
                                                                      call before disk operation fdc.
                                                                      name==0; serch null dir for write
                                                                      name==1; only read dir area with disk_name
                                                                      if ext's msb is 1, High byte of ext is ignored.
FUNCTION No. 154  set_files    : set name                    err = set_files(stat,name); ESC or PLAY
                                                              int stat; file status (DATA,PROG,etc)
                                                              char name[12]; file name area MUST be 12
                                                              remark: if stat[0]'s High byte of ext is ignored.
FUNCTION No. 155  catserch     : blank cat serch             catset bit
FUNCTION No. 156  catreset     : catreset position           catreset bit
FUNCTION No. 157  save         : data transfer from wavemem to device
                                                              save(dev,sta,name); save mode (SAVE only)
                                                              int dev;  device number
                                                              int sta;  files status
                                                              char*name; file name
FUNCTION No. 158  load         : data transfer from device to wavemem
                                                              load(mode,dev,sta,name); load mode (LOAD,MERGE,VER)
FUNCTION No. 159  format_disk_initialize : format_do(disk,pass); disk name
                                                                  char*disk, *pass; pass word
FUNCTION No. 160  format_do    : initialize cat,disk_top dir
FUNCTION No. 161  lcd_initialize : lcd initialize             lcdinit();
FUNCTION No. 162  print        : print string at line,column print(c,l,b,s);
                                                              int c; column pos (0 <= c < 16)
                                                              int l; line pos   (0 <= l < 8)
                                                              int b; back color 0=normal print, 1=reverse, 2=reverse&pri
                                                              char *s; null terminated string
FUNCTION No. 163  cls          : clear screen
FUNCTION No. 164  pset         : dot set/reset               pset(x,y,c);
                                                              int x,y; graphic position
                                                              int c;   0=white, 1=black, 2=ex; MSB=1, no send to lcd
                                                                       0=clear reset dot
                                                                       1=black set dot
                                                                       2=exclusive_dot
FUNCTION No. 165  line         : draw line                   line(xs,ys,xe,ye,c);
                                                              int xs,ys; left top graphic pos
                                                              int xe,ye; right bottom pos
                                                              int c;     0=white, else=black; MSB=1, no send to lcd
                                                                         0=clear (0<=x<=95 && 0<=y<=63) broke memory
FUNCTION No. 167  lcd_vol      : lcd volume set, move constrast  lcd_vol();
FUNCTION No. 168  bdelete      : delete bankdata & voicedata using in bank
                                                              bdelete(bstep,bn); int bstep; delete from bstep
                                                              struct bankdata *bn;
                                                              remark: Delete bank & voice in bank if a voice in bn bank
                                                                      is used by other bank, then not delete this voice.
                                                                      The bstep is a offset. Should be delete voice between
                                                                      bstep and bn->bstep.
FUNCTION No. 169  wdelete      : wave data delete             wdelete(vn); voice number
                                                              struct voicedata *vn;
                                                              remark: Delete voice and wave data. If vn voice is used
                                                                      by other voice, then not delete wave data.
FUNCTION No. 170  wunuse       : delete wave unused part      wunuse(vn);
                                                              struct voicedata *vn; long *end;
FUNCTION No. 171  wend         : check end of wave data       wend(); Return end of wave memory; Not check with memsize
FUNCTION No. 172  wsame        : check same wave point data   match = wsame(vn);
                                                              ERROR: vn is only one, OK: voice use twise.
FUNCTION No. 173  ad_adjust    : match_voice_with_vn          int match;
FUNCTION No. 174  ad_chk       : address check and repair by memsize
                                                              ad_chk(v); struct voicedata *v; long l;
                                                                                                adjust offset (WORD..)
FUNCTION No. 178  merge_bank   : merge source bankdata into dest bankdata
                                                              merge_bank(d,s); struct bankdata *d; merge source
                                                                                struct bankdata *s;
FUNCTION No. 181  use_wave     : use wave no. vp use number   err = use_wave(vp);  int vp;  serch voice #
                                                              0   = vp is null voice
                                                              1   = first use in voice
                                                              2-64= second use
FUNCTION No. 182  use_voice    : check voice vp's use number  err = use_voice(vp,bb);  int vp; serch voice #
                                                              0   = no use voice in bank bb or not first use in voice
                                                              1   = first use in voice
                                                              2-64= second use
FUNCTION No. 183  in_bank      : check voice vp, whether use in bank bb or not
                                                              err = in_bank(vp,bb);   int vp;  serch voice #
                                                                                       int bb;  serch bank #
                                                              ERROR : no use voice in bank b
                                                              OK    : use in bank
FUNCTION No. 184  set_wbvnum   : preset saving wave,bank,voice number
                                                              length = set_wbvnum(sta);
                                                              int length;  data blocknumber (1024byte)
                                                              int sta;     data status (DATA, BANK, VOI)
FUNCTION No. 188  draw_boxline : draw_boxline                 boxline(xs,ys,xe,ye,c);
                                                              int xs,ys; left top graphic pos
                                                              int xe,ye; right bottom pos
                                                              int c;     0=white, else=black; broke memory
                                                                         if (0<=x<=95 && 0<=y<=63) broke memory
FUNCTION No. 189  err          : err = lcd(s);                int err;  err=OK, err=ERROR timeout
                                                              char *s; 0xFF terminated string
                                                              remark: Use outp.inp
FUNCTION No. 190  dido         : disenable all interrupt; close time,midi,key   dido();
FUNCTION No. 191  eido         : disenable all interrupt; open time,midi,key    eido();
FUNCTION No. 192  cread        : physical cluster read       err = cread(pcl,data);
                                                              int pcl;  cluster #
                                                              char *data; data buf
                                                              int err;    read err
                                                              remark: datasize must be greater than FDDSIZ (1024 byte)
FUNCTION No. 194  cwrite       : physical cluster write      err = cwrite(pcl,data);
                                                              int pcl;  cluster #
                                                              char *data; data buf
                                                              int err;    write err
                                                              remark: datasize must be greater than FDDSIZ
FUNCTION No. 195  fdc_format   : fdd first fdc format         err = fdc_format();  int err; error flag
FUNCTION No. 196  seek         : seek hed at c cylinder       seek(c); int c; cylinder number
FUNCTION No. 199  fdc_init     : initialize fdd contlo        fdc_init();
FUNCTION No.  -   fdc_check    : check fdc status             st3 = fdc_check();   int st3;
                                                              0 = OK no error for read & write
                                                             -5 = write protected
                                                             -6 = not double side disk
                                                             -7 = not ready, or motor broken
                                                             -1 = device fault, FDD broken
FUNCTION No. 201  movmem_by_BKM : movmem(s,d,n)               char *s;  source pointer
                                                              char *d;  destination pointer
                                                              unsigned n; byte number (even)
FUNCTION No. 202  cmpmem       : cmp memory by BKM            cmpmem(s,d,n);  char *s; source pointer
                                                              char *d;  destination pointer
                                                              unsigned n; byte number (even)
FUNCTION No. 203  setmem       : set memory by data           setmem(s,n,d);  char *s;  set destination data pointer
                                                              unsigned n;  set number;
                                                              int d;       set data value
FUNCTION No. 204  wpeek        : wave data physical read     wpeek(ad,data,len);  long ad;  wave ram address (WORD)
                                                              char *data;  read buffer point
                                                              unsigned int l;  data length (BYTE even)
FUNCTION No. 205  wpoke        : wave data physical write    wpoke(ad,data,len);  long ad;  wave ram address (WORD)
                                                              char *data;  wave buffer point
                                                              unsigned int l;  data length (BYTE even)
FUNCTION No. 206  wput         : wave data physical read     wput(ad);    long ad; wave ram address (WORD)
                                                              data = wput(ad);   write data
FUNCTION No. 207  wget         : wave data physical read     wget(ad);    long ad; wave ram address (WORD)
                                                              int data = wget(ad);  write data
FUNCTION No. 208  ucomp        : wave data physical verify comparator
                                                              char *data;  read buffer point
                                                              unsigned int l;  data length (BYTE even)
FUNCTION No. 209  led          : set led light
FUNCTION No. 210  cnvdec       : convert unsigned binary into decimal character
                                                              cnvdec(d,c,s);  unsigned int d; convert data
                                                                              int c; character count (2-6)
                                                              char *s;
FUNCTION No. 211  set_uid      : serch wave data max & min between pos0 & pos1
                                                              set_wid(qmax,qmin,pos0,pos1);
                                                              int *qmax,*qmin;  long pos0,pos1; (WORD address)
FUNCTION No. 212  iocreat      : io port open for write      iocreat(mode,sta,&fbuf,&work);
FUNCTION No. 214  ioopen       : io port open for write      ioopen(mode,sta,&fbuf,&work);
FUNCTION No. 215  ininit       : load mode initialize         ininit(); save mode initialize 1024byte
                                                              ininit(work,byte); 1024byte buffer
                                                              char *work;
FUNCTION No. 216  outinit      : save mode initialize        outinit(work,byte);  00004,00008  1024byte buffer
                                                              char *work;
FUNCTION No. 217  iowrite      : io output 1024byte          iowrite(work,byte);  00004,00008
                                                              char *work; 1024byte buffer
FUNCTION No. 219  midiopen     : midiopen(mode,stat,buf,work);
                                                              int mode;  LOAD,MERGE,SAVE,VERIFY
                                                              int stat;  DATA,BANK,VOICE,EFECT
                                                              char *buf;  256 byte buffer
                                                              char *work; 1024byte buffer
FUNCTION No. 220  midcreat     : midi io output initialize
                                                              midcreat(mode,stat,buf,work);
                                                              int mode; LOAD,MERGE,SAVE,VERIFY
                                                              int stat; DATA,BANK,VOICE,EFECT
                                                              char *buf; 256byte buffer
                                                              char *work; 1024byte buffer
FUNCTION No. 221  midiread     : master io input 1024byte    midiread(buf,work);
                                                              char *buf;  256 byte buffer
                                                              char *work; 1024byte buffer
FUNCTION No. 222  midiwrite    : master io output 1024byte   midiwrite(buf,work);
FUNCTION No. 223  midipeek     : data into buf_X_byte
                                                              dataread(buf,byte);  char *buf;  256byte buffer
FUNCTION No. 224  midipoke     : midi exclusive_data_for_mod  midipoke(buf,byte);  char *buf;  256 byte buffer
FUNCTION No. 225  midiclose    : master io output 1024byte   midiclose(buf);
FUNCTION No. 226  mportout     : output_port_data with check_sum
                                                              mportout(buf,byte); char *buf;  send data pointer
                                                              int byte;  send data count (byte)
FUNCTION No. 228  sportin      : sportin(buf,byte);  char *buf; send data pointer
                                                              int byte; send data count (byte)
FUNCTION No. 230  tune_mode    : tune mode define            tune_mode(mode);  int mode; tuning mode
                                                                                          TMOD; transpose mode
                                                                                          RMOD; tunning mode
FUNCTION No. 232  lcut         : limitter v between max and min
                                                              lcut(v,min,max);   long *v; long min,max;
FUNCTION No. 233  lswap        : limitter v between max and min Swap long data
                                                              lswap(a,b);  long *a,*b;
FUNCTION No. 234  ck_lcd       : full set dot                 ck_lcd();
FUNCTION No. 235  ckdisk       : disk checker, check disk sector read/write
                                                              ckdisk();
```

---

## Appendix: Hardware Schematic — uPD71055C-1 (8255 Mode 2)

External port interface chip pinout used by the FZ-1 for parallel I/O. The schematic shows the uPD71055C-1 in 8255 Mode 2 connected to the CPU bus and to the external connector with pull-ups (10K to VDD), termination resistors (1K, 330R), and LS368 buffer ICs for the handshake lines `IBF`, `STBI`, `STBO`, `ACKI`, `ACKO`, `IF IN`, and `IF OUT`. Lines `IF0`–`IF7` carry the 8-bit bidirectional data. The full pinout corresponds to:

```
Port 0:  P00 (pin  4)  IF0
         P01 (pin  3)  IF1
         P02 (pin  2)  IF2
         P03 (pin  1)  IF3
         P04 (pin 40)  IF4 / ACKO
         P05 (pin 39)  IF5
         P06 (pin 38)  IF6
         P07 (pin 37)  IF7
Port 2:  P25 (pin 12)  IBF
         P20 (pin 14)  STBE
         P24 (pin 13)  ACKE / STBO
         P26 (pin 11)  ACKE
         P21 (pin 15)  STBO / OBF
         P27 (pin 10)  ACKO
         P16 (pin 24)  IF IN  / IF OUT
         P22 (pin 16)  BUSY
CPU bus: RESET (pin 35), VDD (pin 26), GND (pin 7),
         A1, A0, CS, WR, RD, D0..D7
```

---

*End of document.*
