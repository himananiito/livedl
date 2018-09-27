use strict;
use warnings;
use v5.20;

open my $f, "-|", "livedl", "-h" or die;
undef $/;
my $s = <$f>;
close $f;

$s =~ s{livedl\s*\((\d+\.\d+)[^\r\n]*}{livedl ($1)} or die;
my $ver = $1;

$s =~ s{chdir:[^\n]*\n}{};

open my $g, "changelog.txt" or die;
my $t = <$g>;
close $g;

$t =~ s{\$latest}{$ver} or die;

open my $h, ">", "changelog.txt" or die;
print $h $t;
close $h;

open my $o, ">", "Readme.txt" or die;
say $o $s;
say $o "";
say $o $t;
close $o;
