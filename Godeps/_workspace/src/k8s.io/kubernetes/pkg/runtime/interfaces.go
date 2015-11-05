/*
Copyright 2014 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package runtime

import (
	"io"
)

// Codec defines methods for serializing and deserializing API objects.
type Codec interface {
	Decoder
	Encoder
}

// Decoder defines methods for deserializing API objects into a given type
type Decoder interface {
	Decode(data []byte) (Object, error)
	// TODO: Remove this method?
	DecodeToVersion(data []byte, version string) (Object, error)
	DecodeInto(data []byte, obj Object) error
	// TODO: Remove this method?
	DecodeIntoWithSpecifiedVersionKind(data []byte, obj Object, kind, version string) error

	// TODO: Add method for processing url parameters.
	// DecodeParametersInto(parameters url.Values, obj Object) error
}

// Encoder defines methods for serializing API objects into bytes
type Encoder interface {
	Encode(obj Object) (data []byte, err error)
	EncodeToStream(obj Object, stream io.Writer) error

	// TODO: Add method for processing url parameters.
	// EncodeParameters(obj Object) (url.Values, error)
}

// ObjectCodec represents the common mechanisms for converting to and from a particular
// binary representation of an object.
// TODO: Remove this interface - it is used only in CodecFor() method.
type ObjectCodec interface {
	Decoder

	// EncodeToVersion convert and serializes an object in the internal format
	// to a specified output version. An error is returned if the object
	// cannot be converted for any reason.
	EncodeToVersion(obj Object, outVersion string) ([]byte, error)
	EncodeToVersionStream(obj Object, outVersion string, stream io.Writer) error
}

// ObjectDecoder is a convenience interface for identifying serialized versions of objects
// and transforming them into Objects. It intentionally overlaps with ObjectTyper and
// Decoder for use in decode only paths.
// TODO: Consider removing this interface?
type ObjectDecoder interface {
	Decoder
	// DataVersionAndKind returns the version and kind of the provided data, or an error
	// if another problem is detected. In many cases this method can be as expensive to
	// invoke as the Decode method.
	DataVersionAndKind([]byte) (version, kind string, err error)
	// Recognizes returns true if the scheme is able to handle the provided version and kind
	// of an object.
	Recognizes(version, kind string) bool
}

///////////////////////////////////////////////////////////////////////////////
// Non-codec interfaces

// ObjectConvertor converts an object to a different version.
type ObjectConvertor interface {
	Convert(in, out interface{}) error
	ConvertToVersion(in Object, outVersion string) (out Object, err error)
	ConvertFieldLabel(version, kind, label, value string) (string, string, error)
}

// ObjectTyper contains methods for extracting the APIVersion and Kind
// of objects.
type ObjectTyper interface {
	// DataVersionAndKind returns the version and kind of the provided data, or an error
	// if another problem is detected. In many cases this method can be as expensive to
	// invoke as the Decode method.
	DataVersionAndKind([]byte) (version, kind string, err error)
	// ObjectVersionAndKind returns the version and kind of the provided object, or an
	// error if the object is not recognized (IsNotRegisteredError will return true).
	ObjectVersionAndKind(Object) (version, kind string, err error)
	// Recognizes returns true if the scheme is able to handle the provided version and kind,
	// or more precisely that the provided version is a possible conversion or decoding
	// target.
	Recognizes(version, kind string) bool
}

// ObjectCreater contains methods for instantiating an object by kind and version.
type ObjectCreater interface {
	New(version, kind string) (out Object, err error)
}

// ObjectCopier duplicates an object.
type ObjectCopier interface {
	// Copy returns an exact copy of the provided Object, or an error if the
	// copy could not be completed.
	Copy(Object) (Object, error)
}

// ResourceVersioner provides methods for setting and retrieving
// the resource version from an API object.
type ResourceVersioner interface {
	SetResourceVersion(obj Object, version string) error
	ResourceVersion(obj Object) (string, error)
}

// SelfLinker provides methods for setting and retrieving the SelfLink field of an API object.
type SelfLinker interface {
	SetSelfLink(obj Object, selfLink string) error
	SelfLink(obj Object) (string, error)

	// Knowing Name is sometimes necessary to use a SelfLinker.
	Name(obj Object) (string, error)
	// Knowing Namespace is sometimes necessary to use a SelfLinker
	Namespace(obj Object) (string, error)
}

// All api types must support the Object interface. It's deliberately tiny so that this is not an onerous
// burden. Implement it with a pointer receiver; this will allow us to use the go compiler to check the
// one thing about our objects that it's capable of checking for us.
type Object interface {
	// This function is used only to enforce membership. It's never called.
	// TODO: Consider mass rename in the future to make it do something useful.
	IsAnAPIObject()
}
